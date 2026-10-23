// Package gb converts Game Boy and Game Boy Color battery saves between emulators.
//
// A battery save is the cartridge's own RAM, and its clock when it has one. Every emulator keeps the
// same RAM; they differ in how they store it: padded to a bank or not, MBC2's half-bytes one or two
// to a byte, MBC7's 16-bit words in either byte order, and each its own clock file. Save holds what
// the cartridge holds, and each Format reads and writes one emulator's files.
//
// The cartridge header says what a save holds, and nothing else does: an 8 KiB file may be 8 KiB
// of RAM, 2 KiB padded, or MBC2's 512 half-bytes as Gambatte keeps them. So every conversion takes
// the ROM's header.
package gb

import (
	"errors"
	"fmt"
	"strings"
)

// HeaderSize is how much of the ROM ParseCart reads.
const HeaderSize = 0x150

// Mapper is the chip a cartridge banks its ROM and RAM with.
type Mapper int

const (
	NoMapper Mapper = iota
	MBC1
	MBC2
	MBC3
	MBC5
	MBC6
	MBC7
	MMM01
	PocketCamera
	TAMA5
	HuC1
	HuC3
	UnknownMapper
)

var mapperNames = [...]string{"ROM only", "MBC1", "MBC2", "MBC3", "MBC5", "MBC6", "MBC7", "MMM01",
	"Pocket Camera", "TAMA5", "HuC1", "HuC3", "unknown mapper"}

func (m Mapper) String() string { return mapperNames[m] }

// Cart is what a cartridge header says about the cartridge's save.
type Cart struct {
	// Title and GameCode are the game's, the code only on later Game Boy Color cartridges.
	Title, GameCode string
	// Type is the header's cartridge type byte (0x147); ROMSize and RAMSize its size codes (0x148,
	// 0x149).
	Type, ROMSize, RAMSize byte
	// CGB is set for games made for the Game Boy Color, alone or as well as the Game Boy.
	CGB bool
}

// ErrNotHeader means data doesn't start with a Game Boy cartridge header.
var ErrNotHeader = errors.New("not a Game Boy cartridge header")

// nintendoLogo is the start of the logo every cartridge carries at 0x104, which the boot ROM checks.
var nintendoLogo = []byte{0xCE, 0xED, 0x66, 0x66, 0xCC, 0x0D, 0x00, 0x0B}

// ParseCart reads a cartridge header: the first HeaderSize bytes of the ROM, or more.
func ParseCart(header []byte) (Cart, error) {
	if len(header) < HeaderSize || string(header[0x104:0x10C]) != string(nintendoLogo) {
		return Cart{}, ErrNotHeader
	}
	var sum byte
	for _, b := range header[0x134:0x14D] {
		sum -= b + 1
	}
	if sum != header[0x14D] {
		return Cart{}, fmt.Errorf("%w: header checksum %#02x, expected %#02x", ErrNotHeader, header[0x14D], sum)
	}
	cgb := header[0x143]&0x80 != 0
	// The title is 16 bytes on the Game Boy. Color games give the last to the CGB flag, and those
	// with a new licensee code (0x14B = 0x33) four more to a game code.
	title, code := header[0x134:0x144], []byte(nil)
	if cgb {
		title = title[:15]
		if header[0x14B] == 0x33 && isCode(title[11:15]) {
			title, code = title[:11], title[11:15]
		}
	}
	clean := func(b []byte) string { return strings.TrimRight(string(b), "\x00 ") }
	return Cart{
		Title:    clean(title),
		GameCode: clean(code),
		Type:     header[0x147],
		ROMSize:  header[0x148],
		RAMSize:  header[0x149],
		CGB:      cgb,
	}, nil
}

func isCode(b []byte) bool {
	for _, c := range b {
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// Mapper is the cartridge's mapper.
func (c Cart) Mapper() Mapper {
	switch t := c.Type; {
	case t == 0x00 || t == 0x08 || t == 0x09:
		return NoMapper
	case t >= 0x01 && t <= 0x03:
		return MBC1
	case t == 0x05 || t == 0x06:
		return MBC2
	case t >= 0x0B && t <= 0x0D:
		return MMM01
	case t >= 0x0F && t <= 0x13:
		return MBC3
	case t >= 0x19 && t <= 0x1E:
		return MBC5
	case t == 0x20:
		return MBC6
	case t == 0x22:
		return MBC7
	case t == 0xFC:
		return PocketCamera
	case t == 0xFD:
		return TAMA5
	case t == 0xFE:
		return HuC3
	case t == 0xFF:
		return HuC1
	}
	return UnknownMapper
}

// Battery reports whether the cartridge keeps its RAM with the power off, so that it has a save.
func (c Cart) Battery() bool {
	switch c.Type {
	case 0x03, 0x06, 0x09, 0x0D, 0x0F, 0x10, 0x13, 0x1B, 0x1E, 0x20, 0x22, 0xFC, 0xFD, 0xFE, 0xFF:
		return true
	}
	return false
}

// Clock reports whether the cartridge has an MBC3 real-time clock.
func (c Cart) Clock() bool { return c.Type == 0x0F || c.Type == 0x10 }

// ramSizes are the RAM sizes the header's codes stand for.
var ramSizes = map[byte]int{0: 0, 1: 0x800, 2: 0x2000, 3: 0x8000, 4: 0x20000, 5: 0x10000}

// SaveSize is how many bytes Save.RAM holds for the cartridge: its RAM's size, MBC2's 512 half-bytes
// one to a byte, or MBC7's EEPROM. Zero for a cartridge with no battery.
func (c Cart) SaveSize() (int, error) {
	if !c.Battery() {
		return 0, nil
	}
	switch c.Mapper() {
	case MBC2:
		return mbc2Cells, nil
	case MBC7:
		return c.mbc7Size(), nil
	}
	n, ok := ramSizes[c.RAMSize]
	if !ok {
		return 0, fmt.Errorf("%w: RAM size code %#02x", ErrUnsupportedCart, c.RAMSize)
	}
	return n, nil
}

// mbc7Size is the size of an MBC7 cartridge's EEPROM: a 93LC56 (128 words) on Kirby Tilt 'n'
// Tumble, the only one with a 1 MiB ROM, and a 93LC66 (256 words) on Command Master.
func (c Cart) mbc7Size() int {
	if c.ROMSize == 0x05 {
		return 0x100
	}
	return 0x200
}
