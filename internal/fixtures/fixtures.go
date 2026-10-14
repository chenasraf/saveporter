// Package fixtures builds synthetic save states for tests: the same shapes the emulators write,
// with no ROM or game data in them.
package fixtures

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"slices"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

const (
	mgbaStateSize = 0x61000
	extHeaderSize = 16
	saveSize      = 0x20000
	// ROMCRC is the ROM CRC32 every fixture state records.
	ROMCRC = 0x27B2E7AE
	// Title and GameCode are the cartridge header every fixture state records.
	Title, GameCode = "POKEMON EMER", "BPEE"
)

// MGBAState is a raw mGBA GBA state of the given version, every byte of its block patterned, with
// a cartridge save and a clock as extra data.
func MGBAState(version uint32) []byte {
	s := make([]byte, mgbaStateSize)
	for i := range s {
		s[i] = byte(i * 7)
	}
	binary.LittleEndian.PutUint32(s, 0x01000000+version)
	binary.LittleEndian.PutUint32(s[8:], ROMCRC)
	copy(s[0x10:0x1C], Title)
	copy(s[0x1C:0x20], GameCode)
	save := bytes.Repeat([]byte{0xA5}, saveSize)
	rtc := bytes.Repeat([]byte{0x11}, 16)
	offset := mgbaStateSize + 3*extHeaderSize
	for _, e := range []struct {
		tag  uint32
		data []byte
	}{{2, save}, {4, rtc}} {
		s = binary.LittleEndian.AppendUint32(s, e.tag)
		s = binary.LittleEndian.AppendUint32(s, uint32(len(e.data)))
		s = binary.LittleEndian.AppendUint64(s, uint64(offset))
		offset += len(e.data)
	}
	s = append(s, make([]byte, extHeaderSize)...)
	s = append(s, save...)
	return append(s, rtc...)
}

// GBTitle is the cartridge title every Game Boy fixture records.
const GBTitle = "FIXTURE"

// MGBAGBState is a raw mGBA Game Boy state of the given version, its block patterned, with a
// cartridge save as extra data. cgb sets the Game Boy Color model.
func MGBAGBState(version uint32, cgb bool) []byte {
	const size = 0x11800
	s := make([]byte, size)
	for i := range s {
		s[i] = byte(i * 3)
	}
	binary.LittleEndian.PutUint32(s, 0x00400000+version)
	binary.LittleEndian.PutUint32(s[4:], ROMCRC)
	s[8] = 0
	if cgb {
		s[8] = 0x80
	}
	copy(s[0x10:0x20], make([]byte, 16))
	copy(s[0x10:], GBTitle)
	save := bytes.Repeat([]byte{0x5A}, 0x8000)
	s = binary.LittleEndian.AppendUint32(s, 2)
	s = binary.LittleEndian.AppendUint32(s, uint32(len(save)))
	s = binary.LittleEndian.AppendUint64(s, size+2*extHeaderSize)
	s = append(s, make([]byte, extHeaderSize)...)
	return append(s, save...)
}

// GambatteState is a Gambatte Game Boy Color state with the records every build writes, patterned
// RAM, and the network link's and Sachen mapper's records when asked, in label order as Gambatte
// writes them.
func GambatteState(netLink, sachen bool) []byte {
	type rec struct {
		label string
		value []byte
	}
	recs := []rec{
		{"a", []byte{1}}, {"hram", bytes.Repeat([]byte{0x22}, 0x200)}, {"minintt", []byte{0, 0, 0, 4}},
		{"nm0irq", []byte{0, 2}}, {"pc", []byte{0x01, 0x50}}, {"rtcs", []byte{7}}, {"sp", []byte{0xDF, 0xF0}},
		{"sram", bytes.Repeat([]byte{0x33}, 0x2000)}, {"vram", bytes.Repeat([]byte{0x44}, 0x4000)},
		{"wram", bytes.Repeat([]byte{0x55}, 0x8000)},
	}
	if netLink {
		recs = append(recs, rec{"netsfc", []byte{0x00}}, rec{"netsv", []byte{0xFF}})
	}
	if sachen {
		recs = append(recs, rec{"schnlct", []byte{0x00}}, rec{"schnomk", []byte{0x00}})
	}
	slices.SortFunc(recs, func(a, b rec) int { return strings.Compare(a.label, b.label) })
	s := []byte{0x00, 0x01, 0, 0, 0}
	for _, r := range recs {
		s = append(s, r.label...)
		s = append(s, 0, byte(len(r.value)>>16), byte(len(r.value)>>8), byte(len(r.value)))
		s = append(s, r.value...)
	}
	return s
}

// Zlib compresses data with zlib.
func Zlib(t testing.TB, data []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// RZIP compresses data as RetroArch's RZIP stream does, in chunks of 128 KiB: version 1 is zlib,
// version 2 Zstandard.
func RZIP(t testing.TB, version byte, data []byte) []byte {
	t.Helper()
	const chunk = 128 << 10
	out := []byte("#RZIPv")
	out = append(out, version, '#')
	out = binary.LittleEndian.AppendUint32(out, chunk)
	out = binary.LittleEndian.AppendUint64(out, uint64(len(data)))
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = enc.Close() }()
	for i := 0; i < len(data); i += chunk {
		part := data[i:min(i+chunk, len(data))]
		var z []byte
		if version == 2 {
			z = enc.EncodeAll(part, nil)
		} else {
			z = Zlib(t, part)
		}
		out = binary.LittleEndian.AppendUint32(out, uint32(len(z)))
		out = append(out, z...)
	}
	return out
}

// MGBAPNG is a raw state from MGBAState or MGBAGBState as the mGBA app writes it: a screenshot with
// the state and each extra data item in private chunks, the items in reverse tag order.
func MGBAPNG(t testing.TB, raw []byte) []byte {
	t.Helper()
	var img bytes.Buffer
	if err := png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 240, 160))); err != nil {
		t.Fatal(err)
	}
	chunk := func(kind string, body []byte) []byte {
		c := binary.BigEndian.AppendUint32(nil, uint32(len(body)))
		c = append(c, kind...)
		c = append(c, body...)
		return binary.BigEndian.AppendUint32(c, crc32.ChecksumIEEE(append([]byte(kind), body...)))
	}
	block := mgbaStateSize
	if binary.LittleEndian.Uint32(raw)>>24 == 0 {
		block = 0x11800 // a Game Boy state
	}
	extra := chunk("gbAs", Zlib(t, raw[:block]))
	var items [][]byte
	for at := block; binary.LittleEndian.Uint32(raw[at:]) != 0; at += extHeaderSize {
		size := binary.LittleEndian.Uint32(raw[at+4:])
		offset := binary.LittleEndian.Uint64(raw[at+8:])
		body := binary.LittleEndian.AppendUint32(nil, binary.LittleEndian.Uint32(raw[at:]))
		body = binary.LittleEndian.AppendUint32(body, size)
		items = append(items, append(body, Zlib(t, raw[offset:offset+uint64(size)])...))
	}
	for _, item := range slices.Backward(items) {
		extra = append(extra, chunk("gbAx", item)...)
	}
	b := img.Bytes()
	iend := len(b) - 12
	return append(append(append([]byte{}, b[:iend]...), extra...), b[iend:]...)
}

// GBHeader is a Game Boy ROM's first 0x150 bytes for a cartridge of the given type and size codes,
// made for the Game Boy Color when cgb is set.
func GBHeader(cartType, romSize, ramSize byte, cgb bool) []byte {
	h := make([]byte, 0x150)
	copy(h[0x104:], []byte{0xCE, 0xED, 0x66, 0x66, 0xCC, 0x0D, 0x00, 0x0B})
	copy(h[0x134:], "FIXTURE")
	if cgb {
		h[0x143] = 0x80
	}
	h[0x147], h[0x148], h[0x149] = cartType, romSize, ramSize
	var sum byte
	for _, b := range h[0x134:0x14D] {
		sum -= b + 1
	}
	h[0x14D] = sum
	return h
}
