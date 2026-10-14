package retroarch

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/chenasraf/saveporter/internal/fixtures"
)

func TestWrapRoundTrip(t *testing.T) {
	core := fixtures.MGBAState(7)
	wrapped := Wrap(core)
	if !bytes.HasPrefix(wrapped, []byte("RASTATE\x01MEM ")) || !bytes.HasSuffix(wrapped, []byte("END \x00\x00\x00\x00")) {
		t.Fatalf("container starts %q", wrapped[:12])
	}
	got, err := CoreState(wrapped)
	if err != nil || !bytes.Equal(got, core) {
		t.Fatalf("round trip: %v", err)
	}
}

func TestReadEveryShape(t *testing.T) {
	core := fixtures.MGBAState(9)
	for name, c := range map[string]struct {
		file      []byte
		comp      Compression
		container bool
	}{
		"raw core state": {core, Uncompressed, false},
		"container":      {Wrap(core), Uncompressed, true},
		"deflate RZIP":   {fixtures.RZIP(t, 1, Wrap(core)), RZIPDeflate, true},
		"zstd RZIP":      {fixtures.RZIP(t, 2, Wrap(core)), RZIPZstd, true},
	} {
		f, err := Read(c.file)
		if err != nil || !bytes.Equal(f.Core, core) || f.Compression != c.comp || f.Container != c.container {
			t.Errorf("%s: compression %v container %v, %v", name, f.Compression, f.Container, err)
		}
	}
}

func TestReadSkipsOtherBlocks(t *testing.T) {
	file := []byte("RASTATE\x01ACHV\x05\x00\x00\x00abcde\x00\x00\x00MEM \x03\x00\x00\x00\x01\x02\x03")
	got, err := CoreState(file)
	if err != nil || !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestReadRejectsDamage(t *testing.T) {
	whole := fixtures.RZIP(t, 1, Wrap(fixtures.MGBAState(7)))
	for name, file := range map[string][]byte{
		"truncated RZIP":  whole[:len(whole)/2],
		"block too long":  []byte("RASTATE\x01MEM \xff\x00\x00\x00abc"),
		"no core block":   []byte("RASTATE\x01END \x00\x00\x00\x00"),
		"unknown version": []byte("RASTATE\x02MEM \x00\x00\x00\x00"),
	} {
		if _, err := Read(file); !errors.Is(err, ErrNotState) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestReadRefusesChunksPastTheirSize(t *testing.T) {
	// A small zstd chunk that inflates far past the chunk size the header declares.
	bomb := fixtures.RZIP(t, 2, make([]byte, 8<<20))
	binary.LittleEndian.PutUint32(bomb[8:], 4096) // chunk size
	if _, err := Read(bomb); !errors.Is(err, ErrNotState) {
		t.Fatalf("got %v", err)
	}
}
