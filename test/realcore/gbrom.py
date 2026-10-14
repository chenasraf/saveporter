#!/usr/bin/env python3
"""Writes the Game Boy test cartridges the save checks run: gbrom.py <out-dir>

Each cartridge, on boot, reads its whole battery RAM bank by bank, writes each bank's checksum to
WRAM (0xC000 + 4*bank: sum lo, sum hi, xor, bank), then adds one to every byte, so the save the
emulator writes back differs from the one it loaded in a way the check can predict. RTC
cartridges then latch the clock and copy its five registers to 0xC100; HuC3 cartridges copy their
clock's six nibbles there. 0xC1FF = 0x42 marks the program done. MBC2's RAM is 512 nibbles; its cartridge masks every value to the low nibble.

Real games have none of this: they write their save only when the player saves, and read the
clock only in some screens. These cartridges make every core read and write all of it.
"""
import os
import sys

# The boot ROMs check this logo; cores that skip the boot ROM check it too.
LOGO = bytes.fromhex(
    "CEED6666CC0D000B03730083000C000D0008111F8889000E"
    "DCCC6EE6DDDDD999BBBB67636E0EECCCDDDC999FBBB9333E")


def huc3_clock():
    """Reads the HuC3 clock into 0xC100: minutes then days, three nibbles each, low first.

    Latch (0x60), point at register 0 (0x40, 0x50), then six reads (0x10), each command written in
    mode 0x0B and run by a write in mode 0x0D, a read's result read in mode 0x0C. Gambatte
    implements only part of the protocol, but this sequence: there, a command runs as it's written
    (0x0D does nothing), 0x60 starts a read and 0x40 resets it, and each 0x10 shifts out the next
    nibble of the same minutes-then-days count.
    """

    def command(cmd):
        return [0x3E, 0x0B, 0xEA, 0x00, 0x00, 0x3E, cmd, 0xEA, 0x00, 0xA0,
                0x3E, 0x0D, 0xEA, 0x00, 0x00, 0x3E, 0xFE, 0xEA, 0x00, 0xA0]

    code = command(0x60) + command(0x40) + command(0x50)
    code += [0x21, 0x00, 0xC1, 0x06, 0x06]  # ld hl,$C100; ld b,6
    loop = len(code)
    code += command(0x10)
    code += [0x3E, 0x0C, 0xEA, 0x00, 0x00, 0xFA, 0x00, 0xA0, 0xE6, 0x0F, 0x22]
    code += [0x05, 0x20, (loop - (len(code) + 3)) & 0xFF]
    return code


def program(ram_size, mbc2, rtc, mbc1, huc3=False):
    banks = max(1, ram_size // 0x2000)
    span = min(ram_size, 0x2000)
    end_hi = 0xA0 + span // 0x100
    mask = [0xE6, 0x0F] if mbc2 else []
    code = [0xF3, 0x31, 0xF0, 0xDF]  # di; ld sp,$DFF0
    code += [0x3E, 0x0A, 0xEA, 0x00, 0x00]  # ld a,$0A; ld ($0000),a: enable RAM
    if mbc1:
        code += [0x3E, 0x01, 0xEA, 0x00, 0x60]  # RAM banking mode
    code += [0x06, 0x00]  # ld b,0
    bankloop = len(code)
    code += [0x78, 0xEA, 0x00, 0x40]  # ld a,b; ld ($4000),a
    code += [0x21, 0x00, 0xA0, 0x11, 0x00, 0x00, 0x0E, 0x00]  # ld hl,$A000; ld de,0; ld c,0
    inner = len(code)
    code += [0x7E] + mask  # ld a,(hl)
    code += [0xF5, 0x83, 0x5F, 0x30, 0x01, 0x14, 0xF1]  # e += a, carry into d
    code += [0xF5, 0xA9, 0x4F, 0xF1]  # c ^= a
    code += [0x3C] + mask + [0x22]  # ld (hl+),a+1
    code += [0x7C, 0xFE, end_hi]  # ld a,h; cp end
    code += [0x20, (inner - (len(code) + 2)) & 0xFF]
    code += [0x78, 0x87, 0x87, 0x6F, 0x26, 0xC0]  # hl = $C000 + 4*b
    code += [0x73, 0x23, 0x72, 0x23, 0x71, 0x23, 0x70]
    code += [0x04, 0x78, 0xFE, banks]  # inc b; cp banks
    code += [0x20, (bankloop - (len(code) + 2)) & 0xFF]
    if rtc:
        # Wait about 200 frames first, as a game reads its clock some time after boot: Gearboy
        # refreshes its time every 20 frames.
        code += [0x16, 0x08, 0x01, 0x00, 0x00]  # ld d,8; ld bc,0
        wait = len(code)
        code += [0x0B, 0x78, 0xB1, 0x20, 0xFB, 0x15, 0x20, 0xF8]  # dec bc to 0, d times
        assert code[wait + 4] == (wait - (wait + 5)) & 0xFF and code[wait + 7] == (wait - (wait + 8)) & 0xFF
        code += [0x3E, 0x00, 0xEA, 0x00, 0x60, 0x3E, 0x01, 0xEA, 0x00, 0x60]  # latch
        code += [0x21, 0x00, 0xC1, 0x06, 0x08]  # ld hl,$C100; ld b,8
        rtcloop = len(code)
        code += [0x78, 0xEA, 0x00, 0x40, 0xFA, 0x00, 0xA0, 0x22, 0x04, 0x78, 0xFE, 0x0D]
        code += [0x20, (rtcloop - (len(code) + 2)) & 0xFF]
    if huc3:
        code += huc3_clock()
    code += [0x3E, 0x00, 0xEA, 0x00, 0x00]  # disable RAM
    code += [0x3E, 0x42, 0xEA, 0xFF, 0xC1]  # done
    code += [0x18, 0xFE]  # jr $
    return bytes(code)


def cartridge(title, cart_type, ram_code, ram_size, cgb, mbc2=False, rtc=False, mbc1=False, huc3=False):
    # 256 KiB, as real games with banked RAM are: Gearboy 3.7.3 mis-banks RAM on a 32 KiB ROM.
    rom = bytearray(0x40000)
    rom[0x100:0x104] = bytes([0x00, 0xC3, 0x50, 0x01])  # nop; jp $0150
    rom[0x104:0x134] = LOGO
    rom[0x134:0x134 + len(title)] = title.encode()
    rom[0x143] = 0x80 if cgb else 0x00
    rom[0x147], rom[0x148], rom[0x149] = cart_type, 0x03, ram_code
    rom[0x14B] = 0x33
    code = program(ram_size, mbc2, rtc, mbc1, huc3)
    rom[0x150:0x150 + len(code)] = code
    rom[0x14D] = -sum(b + 1 for b in rom[0x134:0x14D]) & 0xFF
    rom[0x14E:0x150] = (sum(rom) & 0xFFFF).to_bytes(2, "big")
    return bytes(rom)


CARTS = {
    # name: (title, type, RAM size code, RAM bytes, options)
    "mbc1-8k": ("SPMBC18K", 0x03, 0x02, 0x2000, {}),
    "mbc1-2k": ("SPMBC12K", 0x03, 0x01, 0x800, {}),
    "mbc1-32k": ("SPMBC132K", 0x03, 0x03, 0x8000, {"mbc1": True}),
    "mbc2": ("SPMBC2", 0x06, 0x00, 0x200, {"mbc2": True}),
    "mbc3-rtc-32k": ("SPMBC3RTC", 0x10, 0x03, 0x8000, {"rtc": True}),
    "mbc5-128k": ("SPMBC5128K", 0x1B, 0x04, 0x20000, {}),
    "huc3-32k": ("SPHUC3", 0xFE, 0x03, 0x8000, {"huc3": True}),
}

def write_all(out):
    os.makedirs(out, exist_ok=True)
    for name, (title, t, code, size, opts) in CARTS.items():
        for model, cgb in (("dmg", False), ("cgb", True)):
            with open(os.path.join(out, f"{name}-{model}.gb"), "wb") as f:
                f.write(cartridge(title, t, code, size, cgb, **opts))


if __name__ == "__main__":
    write_all(sys.argv[1])
