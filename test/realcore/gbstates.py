#!/usr/bin/env python3
"""Gambatte state checks (runs in the rig image; see run-saves.sh).

Gambatte states don't name their game, so each state in /corpus/gbstates pairs with the ROM in
/corpus/gbroms whose name it starts with. For each state, and for a state the EmulatorJS build
writes for each ROM, both ways between the Deck's build (with the network link) and EmulatorJS's:

- the source build refuses the other build's state as is (why the conversion exists);
- the converted state loads in the target build;
- commutation: converting the source build's re-serialization equals the target build's
  re-serialization of the converted state, byte for byte;
- the round trip back to the source build is the identity;
- both builds then play the same frames to the same picture and work RAM.
"""
import os
import struct
import subprocess
import sys
import zipfile
import zlib

SP = "/rig/saveporter"
WORK = "/work/gbstates"
DECK = "/cores/gambatte_libretro.so"
EJS = "/rig/bin/gambatte-ejs.so"
failures = []


def check(ok, what):
    print(("  ok   " if ok else "  FAIL ") + what)
    if not ok:
        failures.append(what)
    return ok


def unrzip(b):
    if not b.startswith(b"#RZIPv"):
        return b
    p, out = 20, b""
    while p < len(b):
        n = struct.unpack("<I", b[p:p + 4])[0]
        out += zlib.decompress(b[p + 4:p + 4 + n])
        p += 4 + n
    return out


def core_state(path):
    b = unrzip(open(path, "rb").read())
    if not b.startswith(b"RASTATE"):
        return b
    p = 8
    while p < len(b):
        tag, n = b[p:p + 4], struct.unpack("<I", b[p + 4:p + 8])[0]
        if tag == b"MEM ":
            return b[p + 8:p + 8 + n]
        p += 8 + ((n + 7) & ~7)
    raise ValueError("no MEM block")


def harness(core, rom, state_in, frames, state_out, inp=False):
    env = dict(os.environ, LD_PRELOAD="/rig/bin/timeshim.so")
    if inp:
        env["HARNESS_INPUT"] = "1"
    r = subprocess.run(["/rig/bin/harness", core, rom, state_in, str(frames), state_out],
                       env=env, capture_output=True, text=True)
    return r.returncode == 0, r.stdout


def convert(src_core, to, out):
    """Converts a raw core state for an emulator; returns the converted raw core state."""
    tmp = out + ".in.state"
    open(tmp, "wb").write(src_core)
    subprocess.run([SP, "--to", to, "-f", tmp, out + ".state"], check=True, capture_output=True)
    return core_state(out + ".state")


def hashes(stdout):
    return [l for l in stdout.splitlines() if l.startswith("wram")]


def both_ways(name, rom, state, src, dst, src_emu, dst_emu):
    d = f"{WORK}/{name}"
    os.makedirs(d, exist_ok=True)
    open(f"{d}/orig", "wb").write(state)
    ok, _ = harness(dst, rom, f"{d}/orig", 0, "-")
    check(not ok, f"{name}: {dst_emu} refuses the {src_emu} state as is")
    conv = convert(state, dst_emu, f"{d}/conv")
    open(f"{d}/conv.raw", "wb").write(conv)
    ok, _ = harness(dst, rom, f"{d}/conv.raw", 0, f"{d}/dst-reser")
    if not check(ok, f"{name}: {src_emu} → {dst_emu} loads"):
        return
    ok, _ = harness(src, rom, f"{d}/orig", 0, f"{d}/src-reser")
    check(ok, f"{name}: {src_emu} reloads its own state")
    down = convert(open(f"{d}/src-reser", "rb").read(), dst_emu, f"{d}/src-reser-conv")
    check(down == open(f"{d}/dst-reser", "rb").read(),
          f"{name}: commutation, convert({src_emu} re-serialized) == {dst_emu} re-serializing the conversion")
    back = convert(conv, src_emu, f"{d}/back")
    check(back == state, f"{name}: {src_emu} → {dst_emu} → {src_emu} is the identity")
    _, a = harness(src, rom, f"{d}/orig", 600, "-", inp=True)
    _, b = harness(dst, rom, f"{d}/conv.raw", 600, "-", inp=True)
    check(hashes(a) == hashes(b) and hashes(a), f"{name}: both builds play 600 frames to the same frame and RAM")


def rom_for(state_name, roms):
    for r in sorted(roms, key=len, reverse=True):
        if state_name.startswith(os.path.splitext(r)[0]):
            return r
    return None


def unpack_rom(path):
    if not zipfile.is_zipfile(path):
        return path
    with zipfile.ZipFile(path) as z:
        for n in z.namelist():
            if os.path.splitext(n)[1].lower() in (".gb", ".gbc"):
                out = f"{WORK}/roms/{os.path.basename(n)}"
                os.makedirs(os.path.dirname(out), exist_ok=True)
                open(out, "wb").write(z.read(n))
                return out
    return None


def main():
    roms = os.listdir("/corpus/gbroms")
    for s in sorted(os.listdir("/corpus/gbstates")):
        r = rom_for(s, roms)
        if not r:
            print(f"{s}: no ROM, skipped")
            continue
        print(s)
        rom = unpack_rom(f"/corpus/gbroms/{r}")
        both_ways(s, rom, core_state(f"/corpus/gbstates/{s}"), DECK, EJS, "retroarch", "emulatorjs")
    for r in sorted(roms):
        rom = unpack_rom(f"/corpus/gbroms/{r}")
        name = f"ejs-{os.path.splitext(r)[0]}"
        print(name)
        os.makedirs(f"{WORK}/{name}", exist_ok=True)
        out = f"{WORK}/{name}/written"
        ok, _ = harness(EJS, rom, "-", 900, out, inp=True)
        if check(ok, f"{name}: EmulatorJS's build writes a state"):
            both_ways(name, rom, open(out, "rb").read(), EJS, DECK, "emulatorjs", "retroarch")
    print(f"\n{len(failures)} failures")
    for f in failures:
        print(" ", f)
    sys.exit(1 if failures else 0)


if __name__ == "__main__":
    main()
