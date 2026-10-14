#!/usr/bin/env python3
"""Accuracy matrix for saveporter's GBA conversions, against the real emulators.

Runs in the image built from this directory's Dockerfile (see run.sh), with:
  /rig/saveporter    a linux/amd64 saveporter build
  /corpus/states     save states to test, searched recursively (any emulator, any system)
  /corpus/roms       the games' ROMs, plain or zipped/7z'd, matched to states by CRC
  /corpus/core.so    optional: another mGBA libretro core to test against (e.g. a Steam Deck's)
  /work              scratch space

Cores: v7 (EmulatorJS 4.2.3's state version), vB (current mGBA), the extra core if given, and the
mGBA 0.10.5 app. The cart clock is frozen (time shim) and input scripted, so runs repeat exactly.
Exits non-zero when any check fails.
"""
import glob, json, os, struct, subprocess, sys, zlib

B = '/rig/bin'
SP = '/rig/saveporter'
W = '/work'
LONG = int(os.environ.get('LONG', '3000'))
CROSS = int(os.environ.get('CROSS', '600'))
ENV = dict(os.environ, HARNESS_DIR=W, LD_PRELOAD=f'{B}/timeshim.so')
CORES = {'v7': f'{B}/core-v7.so', 'vB': f'{B}/core-vB.so'}
if os.path.exists('/corpus/core.so'):
    CORES['extra'] = '/corpus/core.so'
# The core states are loaded into on the RetroArch side: the extra one when given.
RA = 'extra' if 'extra' in CORES else 'vB'
STATE_SIZE = 0x61000
# Bits a v7 loader never reads, which a downgrade keeps for a newer mGBA to read back: the high
# bytes of v7's 16-bit GPIO pin state and direction, and bit 3 (RTC SCK edge) of hardware flags1.
V7_IGNORED = {0x291: 0xFF, 0x293: 0xFF, 0x2BE: 0x08}


def sh(*args, env=ENV):
    p = subprocess.run(args, env=env, capture_output=True, text=True)
    return p.returncode, p.stdout + p.stderr


def sp(to, src, dst, version=None):
    args = [SP, '-q', '-f', '--to', to]
    if version:
        args += ['--state-version', version]
    rc, out = sh(*args, src, dst)
    if rc:
        raise RuntimeError(f'saveporter --to {to} {os.path.basename(src)}: {out.strip()}')


def info(path):
    rc, out = sh(SP, 'info', '--json', path)
    return json.loads(out)[0].get('info') if out.strip().startswith('[') else None


def harness(core, rom, state, frames, out='-', play=False):
    env = dict(ENV, HARNESS_INPUT='1') if play else ENV
    rc, text = sh(f'{B}/harness', CORES[core], rom, state, str(frames), out, env=env)
    frame = next((l.split('frame ')[1] for l in text.splitlines() if 'frame ' in l), None)
    return rc == 0 and 'unserialize: ok' in text, frame, text


def app(*args):
    rc, text = sh(f'{B}/sa', *args)
    frame = next((l.split('frame ')[1] for l in text.splitlines() if l.startswith('frame ')), None)
    return rc == 0, frame, text


def rd(p):
    with open(p, 'rb') as f:
        return f.read()


def ext(b):
    items, e = {}, STATE_SIZE
    while e + 16 <= len(b):
        t, s, o = struct.unpack('<iiq', b[e:e + 16])
        e += 16
        if t == 0:
            break
        items[t] = b[o:o + s]
    return items


def diff(a, b, ignored=None):
    """Offsets in the state block (magic excluded) where a and b differ, beyond ignored bits."""
    ignored = ignored or {}
    return [i for i in range(4, STATE_SIZE) if (a[i] ^ b[i]) & ~ignored.get(i, 0)]


def roms():
    out = {}
    os.makedirs(f'{W}/roms', exist_ok=True)
    for f in glob.glob('/corpus/roms/**/*', recursive=True):
        if not os.path.isfile(f):
            continue
        with open(f, 'rb') as fh:
            magic = fh.read(6)
        paths = [f]
        if magic[:2] == b'PK' or magic == b"7z\xbc\xaf'\x1c":
            d = f'{W}/roms/{len(out)}-{os.path.basename(f)}'
            subprocess.run(['7z', 'x', '-y', f'-o{d}', f], capture_output=True)
            paths = [p for p in glob.glob(f'{d}/**/*', recursive=True) if os.path.isfile(p)]
        for p in paths:
            if os.path.getsize(p) >= 0x10000:
                out.setdefault('%08x' % zlib.crc32(rd(p)), p)
    return out


class Check:
    def __init__(self, name):
        self.name, self.fails, self.notes = name, [], []

    def fail(self, msg):
        self.fails.append(msg)


def check_state(path, n, rommap):
    c = Check(os.path.relpath(path, '/corpus/states'))
    i = info(path)
    if not i:
        return None  # not a GBA state
    c.version, t = i['StateVersion'], f'{W}/{n}'
    rom = rommap.get('%08x' % i['ROMCRC'])

    sp('mgba', path, t + '.core', 'keep')
    sp('emulatorjs', path, t + '.web')
    sp('mgba', t + '.web', t + '.webcore', 'keep')
    sp('retroarch', path, t + '.ra')
    sp('mgba', t + '.ra', t + '.racore', 'keep')
    sp('mgba', path, t + '.app')
    orig, web, ra, appst = rd(t + '.core'), rd(t + '.webcore'), rd(t + '.ra'), rd(t + '.app')
    if info(t + '.web')['StateVersion'] > 7 or info(t + '.app')['StateVersion'] > 7:
        c.fail('web or app state newer than v7')
    if not rd(t + '.web').startswith(b'RASTATE\x01MEM ') or not ra.startswith(b'RASTATE\x01MEM '):
        c.fail('not a RetroArch container')
    if rd(t + '.racore') != orig:
        c.fail('RetroArch conversion changed the core state')
    if web != appst:
        c.fail('web and app outputs differ')
    if c.version <= 7 and web != orig:
        c.fail('a v7 state was changed for the web player')
    sp('emulatorjs', t + '.ra', t + '.web2')
    if rd(t + '.web2') != rd(t + '.web'):
        c.fail('web -> RetroArch -> web changed the file')

    if not rom:
        c.notes.append('no ROM: format checks only')
        return c
    for core, state in (('v7', '.webcore'), (RA, '.racore'), ('vB', '.racore'), (RA, '.webcore')):
        ok, _, out = harness(core, rom, t + state, 1)
        if not ok:
            c.fail(f'{core} core refused {state[1:]}: {out.strip()[-160:]}')
    ok, _, out = app('load', rom, t + '.app', '1', '-')
    if not ok:
        c.fail(f'mGBA 0.10.5 refused it: {out.strip()[-160:]}')

    # Byte-exact: downgrading the RetroArch core's re-serialization == the v7 core's re-serialization
    # of the downgrade, apart from bits v7 ignores.
    harness(RA, rom, t + '.racore', 0, t + '.ra0')
    harness('v7', rom, t + '.webcore', 0, t + '.v70')
    sp('mgba', t + '.ra0', t + '.ra0d')
    a, b = rd(t + '.ra0d'), rd(t + '.v70')
    d = diff(a, b, V7_IGNORED)
    if d or ext(a) != ext(b):
        c.fail(f'downgrade differs from v7 itself: {[hex(x) for x in d[:8]]}, extdata equal={ext(a) == ext(b)}')

    # Lossless round trip over long play: original vs RetroArch(web(original)).
    sp('retroarch', t + '.web', t + '.rt')
    sp('mgba', t + '.rt', t + '.rtcore', 'keep')
    okA, fA, _ = harness(RA, rom, t + '.racore', LONG, t + '.longA', play=True)
    okB, fB, _ = harness(RA, rom, t + '.rtcore', LONG, t + '.longB', play=True)
    a, b = rd(t + '.longA'), rd(t + '.longB')
    d = diff(a, b)
    if not (okA and okB) or fA != fB or d or ext(a) != ext(b):
        c.fail(f'round trip diverged after {LONG} frames: {fA} vs {fB}, {[hex(x) for x in d[:8]]}')

    # Informational: the web player's core on the converted state vs the RetroArch core on the original.
    _, f7, _ = harness('v7', rom, t + '.webcore', CROSS, t + '.x7', play=True)
    _, fR, _ = harness(RA, rom, t + '.racore', CROSS, t + '.xR', play=True)
    c.notes.append(f'{CROSS} frames v7 vs {RA}: frame {"same" if f7 == fR else "DIFFERENT"}, '
                   f'{len(diff(rd(t + ".x7"), rd(t + ".xR")))} bytes of core-version drift')
    return c


def check_png(rom, n):
    """The mGBA app's PNG states decode to exactly what it writes raw for the same moment."""
    c = Check(f'mGBA app PNG: {os.path.basename(rom)}')
    for frames in ('300', '1500'):
        p = f'{W}/png-{n}-{frames}.ss1'
        ok, _, _ = app('save', rom, frames, p)
        if not ok or not rd(p).startswith(b'\x89PNG'):
            c.fail('mGBA 0.10.5 did not write a PNG state')
            continue
        sp('mgba', p, p + '.mine', 'keep')
        mine, theirs = rd(p + '.mine'), rd(p + '.mgbaraw')
        ea, eb = ext(mine), ext(theirs)
        ea.pop(0x101, None)  # creation time: the microsecond each write happened
        eb.pop(0x101, None)
        if mine[:STATE_SIZE] != theirs[:STATE_SIZE] or ea != eb:
            c.fail(f'@{frames}: decode differs from mGBA ({len(diff(mine, theirs))} bytes, extdata equal={ea == eb})')
        for core, to in (('v7', 'emulatorjs'), (RA, 'retroarch')):
            sp(to, p, p + '.' + to)
            sp('mgba', p + '.' + to, p + '.' + to + '.core', 'keep')
            ok, _, out = harness(core, rom, p + '.' + to + '.core', 1)
            if not ok:
                c.fail(f'@{frames}: {core} core refused it for {to}')
    return c


def main():
    rommap = roms()
    files = sorted(p for p in glob.glob('/corpus/states/**/*', recursive=True) if os.path.isfile(p))
    print(f'{len(files)} files, {len(rommap)} ROMs, cores {sorted(CORES)} (RetroArch side: {RA})', flush=True)
    checks, used = [], {}
    for n, f in enumerate(files):
        try:
            c = check_state(f, n, rommap)
        except Exception as ex:  # noqa: BLE001
            c = Check(os.path.relpath(f, '/corpus/states'))
            c.fail(f'error: {ex}')
        if c is None:
            continue
        checks.append(c)
        crc = info(f)
        if crc and ('%08x' % crc['ROMCRC']) in rommap:
            used['%08x' % crc['ROMCRC']] = rommap['%08x' % crc['ROMCRC']]
        print(f"{'FAIL' if c.fails else 'ok  '} v{getattr(c, 'version', '?')} {c.name} {c.fails or ''} {'; '.join(c.notes)}", flush=True)
    for n, rom in enumerate(sorted(set(used.values()))):
        c = check_png(rom, n)
        checks.append(c)
        print(f"{'FAIL' if c.fails else 'ok  '} {c.name} {c.fails or ''}", flush=True)
    failed = [c for c in checks if c.fails]
    print(f'\n{len(checks)} checks, {len(failed)} failed')
    sys.exit(1 if failed else 0)


if __name__ == '__main__':
    main()
