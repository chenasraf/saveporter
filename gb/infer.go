package gb

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// ErrNeedHeader means the files alone don't say what the save holds, in a way the conversion
// depends on: the cartridge's header is needed.
var ErrNeedHeader = errors.New("the save's files don't say enough about the cartridge; the ROM's header is needed")

// ErrNotSave means the files aren't a save the format writes for any cartridge.
var ErrNotSave = errors.New("not a save this format writes")

// Convert converts a save from one format's files to another's. cart is the cartridge, from its
// header; nil works it out from the files: every cartridge the source format would have written
// exactly these files for is converted for, and the conversion stands when they all come out the
// same. When they don't (an 8 KiB Gambatte save is plain RAM or MBC2's, which mGBA packs; an 8-byte
// Gambatte clock is an MBC3's or a HuC3's), it fails with ErrNeedHeader.
func Convert(files Files, from, to Format, cart *Cart, now time.Time) (Files, error) {
	if cart != nil {
		s, err := Read(from, *cart, files, now)
		if err != nil {
			return nil, err
		}
		return Write(to, *cart, s, now)
	}
	var outs []output
	var carts, unsupported []Cart
	for _, c := range candidates {
		if !from.wrote(c, files, now) {
			continue
		}
		s, err := Read(from, c, files, now)
		if err != nil {
			continue
		}
		out, err := Write(to, c, s, now)
		if errors.Is(err, ErrUnsupportedCart) {
			// The target runs no such cartridge, so if the game is one, what's written for it
			// doesn't matter.
			unsupported = append(unsupported, c)
			continue
		}
		if err != nil {
			continue
		}
		outs, carts = append(outs, splitOutput(to, c, out)), append(carts, c)
	}
	if len(outs) == 0 && len(unsupported) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotSave, from)
	}
	if len(outs) == 0 {
		return nil, fmt.Errorf("%w: %s keeps none for %s", ErrUnsupportedCart, to, describe(unsupported))
	}
	for _, o := range outs[1:] {
		if !o.loadsLike(outs[0]) {
			return nil, fmt.Errorf("%w: they could be %s", ErrNeedHeader, describe(carts))
		}
	}
	// The shortest of the equivalent outputs: the others only add erased bytes past its end.
	return slices.MinFunc(outs, func(a, b output) int { return len(a.ram) - len(b.ram) }).files, nil
}

// output is a converted save with its RAM and clock apart, the mGBA app's single file split at
// where its RAM ends.
type output struct {
	files      Files
	ram, clock []byte
	hasClock   bool
	// footer is set for the mGBA app's clock, which it reads at the end of the cartridge's RAM.
	footer bool
}

func splitOutput(f Format, c Cart, files Files) output {
	o := output{files: files, ram: files[ExtSRM]}
	o.clock, o.hasClock = files[ExtRTC]
	if sav, ok := files[ExtSav]; ok {
		n, _ := c.SaveSize()
		at := min(f.ramFileSize(c, n), len(sav))
		o.ram, o.clock, o.hasClock = sav[:at], sav[at:], len(sav) > at
		o.footer = o.hasClock
	}
	return o
}

// loadsLike reports whether two outputs load the same: the same clock, and RAM that differs at
// most in erased bytes at its end (no RAM file loads as erased RAM). A clock after the RAM must
// also be where the emulator looks for it.
func (o output) loadsLike(p output) bool {
	if (o.footer || p.footer) && len(o.ram) != len(p.ram) {
		return false
	}
	return sameRAM(o.ram, p.ram) && o.hasClock == p.hasClock && bytes.Equal(o.clock, p.clock)
}

// candidates are the cartridges a save's files can be told apart by: the mappers whose saves the
// formats store differently, at every RAM size. The type bytes stand for their kind: 0x03 any
// cartridge whose save is plain RAM (MBC1, MBC5, HuC1 and the rest), 0x13 MBC3, whose RAM Gearboy
// pads to 32 KiB.
var candidates = func() []Cart {
	var cs []Cart
	for _, ram := range []byte{1, 2, 3, 4, 5} {
		cs = append(cs, Cart{Type: 0x03, RAMSize: ram}, Cart{Type: 0x13, RAMSize: ram},
			Cart{Type: 0x10, RAMSize: ram}, Cart{Type: 0xFE, RAMSize: ram})
	}
	return append(cs,
		Cart{Type: 0x10, RAMSize: 0},                // MBC3 with a clock and no RAM
		Cart{Type: 0x06, RAMSize: 0},                // MBC2
		Cart{Type: 0x22, RAMSize: 0, ROMSize: 0x05}, // MBC7, 128 words
		Cart{Type: 0x22, RAMSize: 0, ROMSize: 0x06}, // MBC7, 256 words
	)
}()

// wrote reports whether the format, saving the cartridge, writes these files: the same RAM file
// and clock files of the sizes it writes.
func (f Format) wrote(c Cart, files Files, now time.Time) bool {
	if f.check(c) != nil {
		return false
	}
	s, err := Read(f, c, files, now)
	if err != nil {
		return false
	}
	again, err := Write(f, c, s, now)
	if err != nil {
		return false
	}
	if f == MGBAApp {
		// The app's clock is its own to write (live registers as of the write, the time of the
		// last latch): only its size is the cartridge's.
		a, b := splitOutput(f, c, again), splitOutput(f, c, Files{ExtSav: files[ExtSav]})
		return bytes.Equal(a.ram, b.ram) && len(a.clock) == len(b.clock)
	}
	if !bytes.Equal(again[ExtSRM], files[ExtSRM]) {
		return false
	}
	rtc, ok := files[ExtRTC]
	switch {
	case !ok:
		// EmulatorJS keeps no clock file, whatever the cartridge.
		return true
	case f == SameBoy:
		// SameBoy writes its clock file for every cartridge with a battery.
		return len(rtc) == sameBoyRTC
	}
	return len(again[ExtRTC]) == len(rtc)
}

// sameRAM reports whether two RAM files load the same: RetroArch copies a file over the core's RAM
// as far as both go and leaves the rest erased.
func sameRAM(a, b []byte) bool {
	return bytes.Equal(bytes.TrimRight(a, "\xff"), bytes.TrimRight(b, "\xff"))
}

func describe(carts []Cart) string {
	var kinds []string
	for _, c := range carts {
		k := c.kind()
		if !slices.Contains(kinds, k) {
			kinds = append(kinds, k)
		}
	}
	if len(kinds) == 1 {
		return kinds[0] + " saves of different RAM sizes"
	}
	return strings.Join(kinds[:len(kinds)-1], ", ") + " or " + kinds[len(kinds)-1]
}

func (c Cart) kind() string {
	switch {
	case c.Clock():
		return "MBC3 with a clock"
	case c.Mapper() == MBC3:
		return "MBC3"
	case c.Mapper() == NoMapper || c.Mapper() == MBC1:
		return "plain RAM"
	}
	return c.Mapper().String()
}
