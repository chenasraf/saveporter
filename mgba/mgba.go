// Package mgba reads and rewrites mGBA's Game Boy Advance save states.
//
// mGBA keeps the console's state in a fixed 0x61000-byte block, followed by tagged extra data
// (the cartridge's save, the clock, metadata). Its libretro core, which RetroArch and EmulatorJS
// run, hands the frontend exactly that "raw" layout. The mGBA app writes the same pieces into a PNG
// of the screen instead, and reads either.
//
// The block's layout carries a version. mGBA loads states of its own version or older and refuses
// newer ones, so a state from a newer mGBA is rewritten as the older version wrote it. Every
// version up to NewestKnownVersion only gave meaning to bytes the older ones left zero or ignore,
// and moved the one field the older ones read, the clock's transfer step, into the pins it follows
// from.
package mgba

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/chenasraf/saveporter/internal/inflate"
)

const (
	// StateSize is the size of the console's state block, before the extra data.
	StateSize = 0x61000

	stateMagic = 0x01000000

	// NewestKnownVersion is the newest state layout this package can rewrite for older mGBAs.
	NewestKnownVersion = 0x0B
	// EmulatorJSVersion is the state version of the mGBA core in EmulatorJS 4.2.3 (the release
	// RomM 5.x ships), the newest it loads.
	EmulatorJSVersion = 0x07
	// AppVersion is the state version of the mGBA app's 0.10 releases, the newest they load.
	AppVersion = 0x07

	extHeaderSize = 16
	extMaxTag     = 0x200

	pinState = 0x290
	// rtcTransferStep is where mGBA before v9 keeps how far a clock transfer has gone, which v9
	// leaves zero and follows from the pins instead.
	rtcTransferStep = 0x298
	rtcStateMachine = 0x09
)

// Reserved bytes each state version gave meaning to, which older versions write as zero. The GPIO
// write latch and flags v8 and v9 put in the high bytes of v7's 16-bit pin state and direction
// stay: v7 reads only the low 4 bits, and a newer mGBA reads them back from a round trip.
var fieldsSince = []struct {
	version  uint32
	from, to int
}{
	{0x0A, 0x3D4, 0x3E0}, // DMA 1 to 3 latches
	{0x0B, 0x3E0, 0x3E8}, // DMA 0 to 3 count latches
}

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

var (
	// ErrNotState means data isn't an mGBA Game Boy Advance save state.
	ErrNotState = errors.New("not an mGBA Game Boy Advance save state")
	// ErrTooNew means a state comes from an mGBA newer than this package knows how to rewrite
	// for an older one.
	ErrTooNew = errors.New("the save state is from a newer mGBA than this version of saveporter knows")
)

// IsPNG reports whether data is a state the mGBA app kept in a PNG.
func IsPNG(data []byte) bool { return bytes.HasPrefix(data, pngSignature) }

// State returns an mGBA Game Boy Advance or Game Boy state in the raw layout, from a raw or PNG
// state.
func State(data []byte) ([]byte, error) {
	if IsPNG(data) {
		var err error
		if data, err = fromPNG(data); err != nil {
			return nil, err
		}
	}
	if _, err := Version(data); err == nil {
		return data, nil
	}
	if _, err := GBVersion(data); err != nil {
		return nil, err
	}
	return data, nil
}

// Version returns the layout version of a raw mGBA Game Boy Advance state.
func Version(state []byte) (uint32, error) {
	if len(state) < StateSize {
		return 0, ErrNotState
	}
	magic := binary.LittleEndian.Uint32(state)
	if magic < stateMagic || magic > stateMagic+0xFF {
		return 0, ErrNotState
	}
	return magic - stateMagic, nil
}

// ROMCRC returns the CRC32 of the ROM a raw state was saved from. mGBA won't load a state into
// another ROM.
func ROMCRC(state []byte) uint32 { return binary.LittleEndian.Uint32(state[8:]) }

// Title returns the game's title and code from the cartridge header, as a raw state recorded them.
func Title(state []byte) (title, code string) {
	clean := func(b []byte) string { return strings.TrimRight(string(b), "\x00 ") }
	return clean(state[0x10:0x1C]), clean(state[0x1C:0x20])
}

// Game Boy and Game Boy Color states, which mGBA keeps in a block of their own with versions of
// their own. Every mGBA in use writes version 3 (the 0.10 app, the libretro core, EmulatorJS 4.2.3's
// core), and mGBA refuses newer versions, so a newer one is refused here rather than rewritten.
const (
	// GBStateSize is the size of a Game Boy state's block, before the extra data.
	GBStateSize = 0x11800
	// GBNewestKnownVersion is the newest Game Boy state layout this package knows.
	GBNewestKnownVersion = 3

	gbStateMagic = 0x00400000
	gbModelCGB   = 0x80
)

// GBVersion returns the layout version of a raw mGBA Game Boy state.
func GBVersion(state []byte) (uint32, error) {
	if len(state) < GBStateSize {
		return 0, ErrNotState
	}
	magic := binary.LittleEndian.Uint32(state)
	if magic < gbStateMagic || magic > gbStateMagic+0xFF {
		return 0, ErrNotState
	}
	return magic - gbStateMagic, nil
}

// IsGB reports whether a raw state is of a Game Boy or Game Boy Color.
func IsGB(state []byte) bool {
	_, err := GBVersion(state)
	return err == nil
}

// GBInfo returns what a raw Game Boy state recorded about its game: whether it ran as a Game Boy
// Color, the cartridge header's title, and the ROM's CRC32.
func GBInfo(state []byte) (cgb bool, title string, crc uint32) {
	return state[8]&gbModelCGB != 0, strings.TrimRight(string(state[0x10:0x20]), "\x00 "),
		binary.LittleEndian.Uint32(state[4:])
}

// GBForVersion returns a raw Game Boy state as an mGBA whose states are of the given version loads
// it.
func GBForVersion(state []byte, version uint32) ([]byte, error) {
	have, err := GBVersion(state)
	if err != nil {
		return nil, err
	}
	if have > version {
		return nil, fmt.Errorf("%w: Game Boy state version %d, newest loadable %d", ErrTooNew, have, version)
	}
	return state, nil
}

// ForVersion returns a raw state as an mGBA whose states are of the given version loads it.
func ForVersion(state []byte, version uint32) ([]byte, error) {
	have, err := Version(state)
	if err != nil {
		return nil, err
	}
	if have <= version {
		return state, nil
	}
	if have > NewestKnownVersion {
		return nil, fmt.Errorf("%w: version %d", ErrTooNew, have)
	}
	out := slices.Clone(state)
	binary.LittleEndian.PutUint32(out, stateMagic+version)
	for _, f := range fieldsSince {
		if f.version > version {
			clear(out[f.from:f.to])
		}
	}
	if have >= rtcStateMachine && version < rtcStateMachine {
		binary.LittleEndian.PutUint32(out[rtcTransferStep:], transferStep(out[pinState]))
	}
	return out, nil
}

// transferStep is the step mGBA before v9 is at with the clock's pins as given: 2 while chip select
// is high (a transfer), 1 when it's low with the clock line high (ready to start one), 0 otherwise.
func transferStep(pins byte) uint32 {
	const sck, cs = 1, 4
	switch {
	case pins&cs != 0:
		return 2
	case pins&sck != 0:
		return 1
	}
	return 0
}

// fromPNG takes the state and its extra data out of a state the mGBA app kept in a PNG.
func fromPNG(data []byte) ([]byte, error) {
	var state []byte
	type extItem struct {
		tag  uint32
		data []byte
	}
	var ext []extItem
	for rest := data[len(pngSignature):]; len(rest) >= 12; {
		size := binary.BigEndian.Uint32(rest)
		if uint64(size) > uint64(len(rest)-12) {
			return nil, fmt.Errorf("%w: PNG chunk runs past the end", ErrNotState)
		}
		kind, body := string(rest[4:8]), rest[8:8+size]
		rest = rest[12+size:]
		switch kind {
		case "gbAs":
			var err error
			if state, err = inflate.Zlib(body, StateSize); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrNotState, err)
			}
		case "gbAx":
			if len(body) < 8 {
				continue
			}
			tag, n := binary.LittleEndian.Uint32(body), binary.LittleEndian.Uint32(body[4:])
			if tag == 0 || tag >= extMaxTag || n > 1<<26 {
				continue
			}
			item, err := inflate.Zlib(body[8:], int(n))
			if err != nil {
				continue // mGBA skips extra data it can't inflate too
			}
			ext = append(ext, extItem{tag, item})
		case "IEND":
			rest = nil
		}
	}
	if state == nil {
		return nil, fmt.Errorf("%w: the PNG holds no state", ErrNotState)
	}
	if len(state) != StateSize && len(state) != GBStateSize {
		return nil, fmt.Errorf("%w: the PNG's state is %d bytes", ErrNotState, len(state))
	}
	if len(ext) == 0 {
		return state, nil
	}

	// The raw layout: the state, a table of (tag, size, offset from the start) closed by a zero
	// entry, then each item's data, in tag order.
	slices.SortStableFunc(ext, func(a, b extItem) int { return int(a.tag) - int(b.tag) })
	offset := len(state) + (len(ext)+1)*extHeaderSize
	out := slices.Grow(state, offset-len(state))
	for _, e := range ext {
		out = binary.LittleEndian.AppendUint32(out, e.tag)
		out = binary.LittleEndian.AppendUint32(out, uint32(len(e.data))) //nolint:gosec // capped above
		out = binary.LittleEndian.AppendUint64(out, uint64(offset))      //nolint:gosec // positive
		offset += len(e.data)
	}
	out = append(out, make([]byte, extHeaderSize)...)
	for _, e := range ext {
		out = append(out, e.data...)
	}
	return out, nil
}
