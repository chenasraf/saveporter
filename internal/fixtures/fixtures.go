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

// MGBAPNG is a raw state from MGBAState as the mGBA app writes it: a screenshot with the state and
// each extra data item in private chunks, the items in reverse tag order.
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
	extra := chunk("gbAs", Zlib(t, raw[:mgbaStateSize]))
	saveAt := mgbaStateSize + 3*extHeaderSize
	for _, e := range []struct {
		tag      uint32
		from, to int
	}{{4, saveAt + saveSize, len(raw)}, {2, saveAt, saveAt + saveSize}} {
		body := binary.LittleEndian.AppendUint32(nil, e.tag)
		body = binary.LittleEndian.AppendUint32(body, uint32(e.to-e.from))
		extra = append(extra, chunk("gbAx", append(body, Zlib(t, raw[e.from:e.to])...))...)
	}
	b := img.Bytes()
	iend := len(b) - 12
	return append(append(append([]byte{}, b[:iend]...), extra...), b[iend:]...)
}
