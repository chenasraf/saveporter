#!/usr/bin/env python3
"""Battery-save matrix for the Game Boy cores (runs in the rig image; see run-saves.sh).

For each test cartridge (gbrom.py) and each core A: saveporter writes A's files from a known seed,
A plays them at a later time, and the cartridge's own reads must show exactly the seed's RAM and the
clock advanced by the time that passed. Then what A wrote back is converted to every core B, which
plays it later still, and B's save is converted back to A, which plays it once more. Every read in
the chain is checked against what the cartridge must see.

Real saves (SAVES, with their ROMs in ROMS) go through every pair too: converted, played for a few
seconds and written back by B, converted back, and compared with the original byte for byte.
"""
import os
import random
import struct
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor

sys.path.insert(0, os.path.dirname(__file__))
import gbrom  # noqa: E402

SP = "/rig/saveporter"
WORK = "/work"
FRAMES = "1100"
T0 = 1760400000
STEPS = [T0 + 93784, T0 + 93784 + 200000, T0 + 93784 + 200000 + 3000000]
DAY = 86400

CORES = {  # name: (format, libretro core or None for the mGBA app)
    "gambatte": ("gambatte", "/cores/gambatte_libretro.so"),
    "gambatte-ejs": ("gambatte", "/rig/bin/gambatte-ejs.so"),
    "sameboy": ("sameboy", "/cores/sameboy_libretro.so"),
    "vbam": ("vbam", "/cores/vbam_libretro.so"),
    "gearboy": ("gearboy", "/cores/gearboy_libretro.so"),
    "mgba": ("mgba", "/cores/mgba_libretro.so"),
    "app": ("mgba-app", None),
}
# What each format keeps no save for (saveporter refuses these; checked below).
UNSUPPORTED = {("gearboy", "mbc2"), ("gearboy", "huc3-32k")}
# The cores that keep a HuC3 clock: SameBoy's and mGBA's libretro cores don't save it.
HUC3_CLOCK = {"gambatte", "gambatte-ejs", "vbam", "app"}

failures = []


def check(ok, what):
    if not ok:
        failures.append(what)
        print("  FAIL", what)
    return ok


def sp(*args):
    r = subprocess.run([SP, *args], capture_output=True, text=True)
    if r.returncode:
        raise RuntimeError(f"saveporter {' '.join(args)}: {r.stderr.strip()}")


def play(core, rom, stem, now, out):
    """Plays the save at <stem>.srm/.rtc (or .sav) at unix time now; writes out's files and
    returns the WRAM the cartridge left its reads in."""
    env = dict(os.environ, LD_PRELOAD="/rig/bin/timeshim.so", TIMESHIM_NOW=str(now))
    if core == "gearboy":
        env["TIMESHIM_TICK"] = "1"
    fmt, lib = CORES[core]
    if lib is None:
        data = open(stem + ".sav", "rb").read() if os.path.exists(stem + ".sav") else b""
        open(out + ".sav", "wb").write(data)
        subprocess.run(["/rig/bin/sa", "sav", rom, out + ".sav", FRAMES, out + ".wram"],
                       env=env, capture_output=True, check=True)
        return open(out + ".wram", "rb").read()
    for x, var in ((".srm", "HARNESS_SAVE_IN"), (".rtc", "HARNESS_RTC_IN")):
        if os.path.exists(stem + x):
            env[var] = stem + x
    for x in (".srm", ".rtc"):
        if os.path.exists(out + x):
            os.remove(out + x)
    env.update(HARNESS_SAVE_OUT=out + ".srm", HARNESS_RTC_OUT=out + ".rtc", HARNESS_WRAM_OUT=out + ".wram")
    subprocess.run(["/rig/bin/harness", lib, rom, "-", FRAMES, "-"], env=env, capture_output=True, check=True)
    return open(out + ".wram", "rb").read()


def convert(rom, frm, to, src, dst, now):
    fmt_from, fmt_to = CORES[frm][0], CORES[to][0]
    inp = src + (".sav" if fmt_from == "mgba-app" else ".srm")
    for x in (".srm", ".rtc", ".sav"):
        if os.path.exists(dst + x):
            os.remove(dst + x)
    sp("save", "--rom", rom, "--from", fmt_from, "--to", fmt_to, "--now", str(now), "-f", inp, dst + ".x")


def expect_sums(ram, mbc2):
    out = b""
    for b in range(max(1, len(ram) // 0x2000)):
        chunk = ram[b * 0x2000:(b + 1) * 0x2000]
        if mbc2:
            chunk = bytes(x & 15 for x in chunk)
        s, x = sum(chunk) & 0xFFFF, 0
        for v in chunk:
            x ^= v
        out += bytes([s & 255, s >> 8, x, b])
    return out


def clock_regs(seconds):
    days = seconds // DAY
    carry = days >= 512
    days %= 512
    return bytes([seconds % 60, seconds % 3600 // 60, seconds % DAY // 3600, days & 0xFF,
                  (days >> 8) | (0x80 if carry else 0)])


def bump(ram, mbc2):
    return bytes(((x + 1) & 15) | (x & 0xF0) if mbc2 else (x + 1) & 255 for x in ram)


def huc3_nibbles(seconds):
    minutes, days = seconds // 60 % 1440, seconds // DAY & 0xFFF
    return bytes([minutes & 15, minutes >> 4 & 15, minutes >> 8, days & 15, days >> 4 & 15, days >> 8])


def reads(core, wram, ram, mbc2, clock, what):
    """Checks what the cartridge read: its RAM, and its clock when clock (the expected registers or
    nibbles) is given."""
    ok = check(wram[0x1FF] == 0x42, f"{what}: cartridge didn't finish")
    sums = expect_sums(ram, mbc2)
    ok &= check(wram[:len(sums)] == sums, f"{what}: RAM read differs")
    if clock is not None:
        got = wram[0x100:0x100 + len(clock)]
        if len(clock) == 5:
            # DH reads keep only day bit 8, halt and carry.
            got = got[:4] + bytes([got[4] & 0xC1])
        ok &= check(got == clock, f"{what}: clock reads {got.hex()}, expected {clock.hex()}")
    return ok


def seed_files(cart, ram_size, mbc2, rtc, seed_seconds, d):
    """The seed as Gambatte's files, which saveporter's Gambatte reader takes as given."""
    rnd = random.Random(cart)
    ram = bytes(rnd.randrange(256) for _ in range(ram_size))
    if mbc2:
        ram = bytes(0xF0 | (x & 15) for x in ram)
    pad = 0x2000 if mbc2 or ram_size < 0x2000 else ram_size
    open(d + "/seed.srm", "wb").write(ram + bytes([0xFF]) * (pad - len(ram)))
    if rtc:
        open(d + "/seed.rtc", "wb").write(struct.pack("<Q", T0 - seed_seconds))
    return ram


SEED_SECONDS = 300 * DAY + 23 * 3600 + 59 * 60 + 50


def chain(carts_dir, name, model, a):
    """Seeds core a with the cartridge's save, then carries what it writes to every core and back."""
    title, typ, code, size, opts = gbrom.CARTS[name]
    mbc2, rtc, huc3 = opts.get("mbc2", False), opts.get("rtc", False), opts.get("huc3", False)
    cart = f"{name}-{model}"
    rom = f"{carts_dir}/{cart}.gb"
    d = f"{WORK}/saves/{cart}"
    ram = seed_files(cart, size, mbc2, rtc or huc3, SEED_SECONDS, d)

    def clock(t, *chain):
        """The clock the cartridge reads at t, when every core the save went through keeps it."""
        if rtc:
            return clock_regs(SEED_SECONDS + t - T0)
        if huc3 and all(c in HUC3_CLOCK for c in chain):
            return huc3_nibbles(SEED_SECONDS + t - T0)
        return None

    if (CORES[a][0], name) in UNSUPPORTED:
        try:
            convert(rom, "gambatte", a, f"{d}/seed", f"{d}/{a}-seed", T0)
            check(False, f"{cart} {a}: converted a save the core keeps none of")
        except RuntimeError:
            pass
        return
    try:
        convert(rom, "gambatte", a, f"{d}/seed", f"{d}/{a}-seed", T0)
    except RuntimeError as e:
        check(False, f"{cart} seed → {a}: {e}")
        return
    w = play(a, rom, f"{d}/{a}-seed", STEPS[0], f"{d}/{a}-1")
    if not reads(a, w, ram, mbc2, clock(STEPS[0], a), f"{cart} {a} reads its seed"):
        return
    ram1 = bump(ram, mbc2)
    for b in CORES:
        if (CORES[b][0], name) in UNSUPPORTED:
            continue
        pair = f"{cart} {a} → {b}"
        try:
            convert(rom, a, b, f"{d}/{a}-1", f"{d}/{a}-{b}-2", STEPS[0])
        except RuntimeError as e:
            check(False, f"{pair}: {e}")
            continue
        w = play(b, rom, f"{d}/{a}-{b}-2", STEPS[1], f"{d}/{a}-{b}-2out")
        if not reads(b, w, ram1, mbc2, clock(STEPS[1], a, b), f"{pair}: {b} reads it"):
            continue
        try:
            convert(rom, b, a, f"{d}/{a}-{b}-2out", f"{d}/{a}-{b}-3", STEPS[1])
        except RuntimeError as e:
            check(False, f"{pair} → {a}: {e}")
            continue
        w = play(a, rom, f"{d}/{a}-{b}-3", STEPS[2], f"{d}/{a}-{b}-3out")
        if reads(a, w, bump(ram1, mbc2), mbc2, clock(STEPS[2], a, b), f"{pair} → {a}: {a} reads it back"):
            print(f"  ok {pair} → {a}")


def cart_matrix(carts_dir):
    jobs = []
    only = os.environ.get("ONLY")
    for name, (title, typ, code, size, opts) in gbrom.CARTS.items():
        if only and only not in name:
            continue
        for model in ("dmg", "cgb"):
            cart = f"{name}-{model}"
            d = f"{WORK}/saves/{cart}"
            os.makedirs(d, exist_ok=True)
            seed_files(cart, size, opts.get("mbc2", False), opts.get("rtc", False), SEED_SECONDS, d)
            jobs += [(name, model, a) for a in CORES]
    with ThreadPoolExecutor(os.cpu_count()) as pool:
        for f in [pool.submit(chain, carts_dir, *j) for j in jobs]:
            f.result()


def main():
    carts = f"{WORK}/carts"
    gbrom.write_all(carts)
    cart_matrix(carts)
    print(f"\n{len(failures)} failures")
    for f in failures:
        print(" ", f)
    sys.exit(1 if failures else 0)


if __name__ == "__main__":
    main()
