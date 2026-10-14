package gb

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"time"
)

// Save is what a cartridge keeps with the power off.
type Save struct {
	// RAM is the cartridge's RAM, Cart.SaveSize bytes. MBC2's RAM is 512 half-bytes, here one to a
	// byte in the low half; the high half holds what the game wrote there when the emulator kept it.
	// MBC7's EEPROM is here as its 16-bit words, big-endian.
	RAM []byte
	// Clock is an MBC3 cartridge's clock, nil when the files have none.
	Clock *Clock
	// HuC3 is a HuC3 cartridge's clock, nil when the files have none.
	HuC3 *HuC3Clock
}

// Files are one save's files, by extension: ".srm" and ".rtc" for RetroArch's cores, ".sav" for the
// mGBA app.
type Files map[string][]byte

// File extensions.
const (
	ExtSRM = ".srm"
	ExtRTC = ".rtc"
	ExtSav = ".sav"
)

// Format is how an emulator keeps a save.
type Format string

const (
	// Gambatte is the Gambatte libretro core: the RAM in a ".srm" padded to whole 8 KiB banks, MBC2
	// one half-byte to a byte in 8 KiB, and a ".rtc" holding the unix time the clock read zero.
	Gambatte Format = "gambatte"
	// SameBoy is the SameBoy libretro core: the RAM as is, and SameBoy's own 32-byte ".rtc".
	SameBoy Format = "sameboy"
	// VBAM is the VBA-M libretro core: the RAM as is, a 48-byte ".rtc" for MBC3, and the unix time
	// the clock read zero for HuC3.
	VBAM Format = "vbam"
	// Gearboy is the Gearboy libretro core (3.7): banks of 8 KiB, 32 KiB for every MBC3 cartridge,
	// and a 48-byte ".rtc".
	Gearboy Format = "gearboy"
	// MGBA is mGBA's libretro core: the RAM in a ".srm" of at least 8 KiB, MBC2 two half-bytes to a
	// byte, and a 48-byte ".rtc". It keeps no HuC3 clock.
	MGBA Format = "mgba"
	// MGBAApp is the mGBA app: MGBA's RAM and clock in one ".sav", the clock after the RAM, and its
	// HuC3 clock too.
	MGBAApp Format = "mgba-app"
)

// Formats lists every format.
var Formats = []Format{Gambatte, SameBoy, VBAM, Gearboy, MGBA, MGBAApp}

var (
	// ErrUnsupportedCart means a format has no save for the cartridge: the emulator doesn't run it,
	// or doesn't keep its save.
	ErrUnsupportedCart = errors.New("the emulator keeps no save for this cartridge")
	// ErrNoSave means the cartridge keeps nothing with the power off.
	ErrNoSave = errors.New("the cartridge has no save")
	// ErrMissingFile means the files lack one the format keeps the save in.
	ErrMissingFile = errors.New("missing save file")
	// ErrUnknownFormat means a Format isn't one this package knows.
	ErrUnknownFormat = errors.New("unknown save format")
)

const (
	bank      = 0x2000
	mbc2Cells = 0x200
	// fill is what erased cartridge RAM reads, and what the emulators fill RAM a file doesn't cover
	// with.
	fill = 0xFF
)

// supports reports whether the format keeps saves for the cartridge.
func (f Format) supports(c Cart) bool {
	m := c.Mapper()
	switch m {
	case MBC6, TAMA5, MMM01, UnknownMapper:
		return false
	}
	switch f {
	case Gambatte:
		// Gambatte runs ROM+RAM cartridges as MBC2.
		return m != MBC7 && m != PocketCamera && (m != NoMapper || c.Type == 0)
	case VBAM:
		// VBA-M runs the Pocket Camera with no battery.
		return m != PocketCamera
	case Gearboy:
		// Gearboy 3.7 keeps 32 KiB for MBC3: MBC30's 64 KiB doesn't fit.
		return m == NoMapper || m == MBC1 || m == MBC5 || m == HuC1 || m == MBC3 && c.RAMSize != 0x05
	case SameBoy, MGBA, MGBAApp:
		return true
	}
	return false
}

// ramFileSize is the size of the format's RAM file for n bytes of the cartridge's RAM.
func (f Format) ramFileSize(c Cart, n int) int {
	m := c.Mapper()
	switch f {
	case Gambatte:
		if m == MBC2 {
			return bank
		}
		switch c.RAMSize {
		case 0x00:
			return 0
		case 0x01, 0x02:
			return bank
		case 0x03:
			return 4 * bank
		}
		return 16 * bank
	case Gearboy:
		if m == MBC3 {
			return 4 * bank
		}
		switch c.RAMSize {
		case 0x00:
			return 0
		case 0x01, 0x02:
			return bank
		case 0x04:
			return 16 * bank
		}
		return 4 * bank
	case MGBA, MGBAApp:
		if m == MBC2 {
			return mbc2Cells / 2
		}
		if m == MBC7 || n == 0 {
			return n
		}
		return max(n, bank)
	}
	return n
}

// ramFile is the format's RAM file for the cartridge's RAM.
func (f Format) ramFile(c Cart, ram []byte) []byte {
	switch c.Mapper() {
	case MBC2:
		if f == MGBA || f == MGBAApp {
			b := make([]byte, mbc2Cells/2)
			for i := range b {
				b[i] = ram[2*i]&0xF | ram[2*i+1]<<4
			}
			return b
		}
	case MBC7:
		if f == SameBoy {
			return swapWords(ram)
		}
	}
	b := bytes.Repeat([]byte{fill}, f.ramFileSize(c, len(ram)))
	copy(b, ram)
	return b
}

// ramFrom reads the cartridge's n bytes of RAM from the format's RAM file. A short file reads as
// erased RAM past its end, as the emulators load one.
func (f Format) ramFrom(c Cart, file []byte, n int) []byte {
	switch c.Mapper() {
	case MBC2:
		if f == MGBA || f == MGBAApp {
			ram := make([]byte, mbc2Cells)
			for i := range ram {
				ram[i] = fill
			}
			for i := 0; i < len(file) && i < mbc2Cells/2; i++ {
				ram[2*i], ram[2*i+1] = 0xF0|file[i]&0xF, 0xF0|file[i]>>4
			}
			return ram
		}
	case MBC7:
		if f == SameBoy {
			file = swapWords(file)
		}
	}
	ram := bytes.Repeat([]byte{fill}, n)
	copy(ram, file)
	return ram
}

func swapWords(b []byte) []byte {
	s := slices.Clone(b)
	for i := 0; i+1 < len(s); i += 2 {
		s[i], s[i+1] = s[i+1], s[i]
	}
	return s
}

// Read reads a save from the format's files. now is the time Gambatte's and VBA-M's clocks, which
// keep only when they read zero, are read at.
func Read(f Format, c Cart, files Files, now time.Time) (Save, error) {
	if err := f.check(c); err != nil {
		return Save{}, err
	}
	n, err := c.SaveSize()
	if err != nil {
		return Save{}, err
	}
	ram, rtc := files[ExtSRM], files[ExtRTC]
	if f == MGBAApp {
		sav, ok := files[ExtSav]
		if !ok {
			return Save{}, fmt.Errorf("%w: %s", ErrMissingFile, ExtSav)
		}
		size := min(f.ramFileSize(c, n), len(sav))
		ram, rtc = sav[:size], sav[size:]
	} else if _, ok := files[ExtSRM]; !ok && f.ramFileSize(c, n) > 0 {
		return Save{}, fmt.Errorf("%w: %s", ErrMissingFile, ExtSRM)
	}
	s := Save{RAM: f.ramFrom(c, ram, n)}
	switch {
	case c.Clock():
		s.Clock = f.readClock(rtc, now)
	case c.Mapper() == HuC3:
		s.HuC3 = f.readHuC3(rtc, now)
	}
	return s, nil
}

func (f Format) readClock(b []byte, now time.Time) *Clock {
	var c Clock
	switch {
	case f == Gambatte && len(b) >= baseTimeSize:
		c = readBaseTime(b, now)
	case f == SameBoy && len(b) >= sameBoyRTC:
		c = readSameBoy(b)
	case (f == VBAM || f == Gearboy) && len(b) >= footerSize:
		c = readFooter(b, false)
	case (f == MGBA || f == MGBAApp) && len(b) >= footerSize:
		c = readFooter(b, true)
	default:
		return nil
	}
	return &c
}

func (f Format) readHuC3(b []byte, now time.Time) *HuC3Clock {
	var h HuC3Clock
	switch {
	case (f == Gambatte || f == VBAM) && len(b) >= baseTimeSize:
		h = readHuC3BaseTime(b, now)
	case f == MGBAApp && len(b) >= huc3Footer:
		h = readHuC3Footer(b)
	default:
		return nil
	}
	return &h
}

// Write writes a save as the format's files. The clocks are written as they read at now, Gambatte's
// as the time they read zero. Files the format has no use for the save in are left out: no ".rtc"
// for a cartridge without a clock, and none for a clock the format doesn't keep.
func Write(f Format, c Cart, s Save, now time.Time) (Files, error) {
	if err := f.check(c); err != nil {
		return nil, err
	}
	n, err := c.SaveSize()
	if err != nil {
		return nil, err
	}
	if len(s.RAM) != n {
		return nil, fmt.Errorf("save RAM is %d bytes, the cartridge's is %d", len(s.RAM), n)
	}
	ram := f.ramFile(c, s.RAM)
	var rtc []byte
	switch {
	case s.Clock != nil && c.Clock():
		rtc = f.writeClock(*s.Clock, now)
	case s.HuC3 != nil && c.Mapper() == HuC3:
		rtc = f.writeHuC3(*s.HuC3, now)
	}
	files := Files{}
	if f == MGBAApp {
		files[ExtSav] = append(ram, rtc...)
		return files, nil
	}
	if len(ram) > 0 {
		files[ExtSRM] = ram
	}
	if rtc != nil {
		files[ExtRTC] = rtc
	}
	return files, nil
}

func (f Format) writeClock(c Clock, now time.Time) []byte {
	switch f {
	case Gambatte:
		return writeBaseTime(c, now)
	case SameBoy:
		return writeSameBoy(c.At(now))
	}
	return writeFooter(c.At(now))
}

func (f Format) writeHuC3(h HuC3Clock, now time.Time) []byte {
	switch f {
	case Gambatte, VBAM:
		return writeHuC3BaseTime(h)
	case MGBAApp:
		return writeHuC3Footer(h.At(now))
	}
	return nil
}

func (f Format) check(c Cart) error {
	if !slices.Contains(Formats, f) {
		return fmt.Errorf("%w: %q", ErrUnknownFormat, f)
	}
	if !c.Battery() {
		return fmt.Errorf("%w: %s (type %#02x) has no battery", ErrNoSave, c.Mapper(), c.Type)
	}
	if !f.supports(c) {
		return fmt.Errorf("%w: %s keeps none for %s (type %#02x)", ErrUnsupportedCart, f, c.Mapper(), c.Type)
	}
	return nil
}
