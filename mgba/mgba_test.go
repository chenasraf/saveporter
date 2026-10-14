package mgba

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/chenasraf/saveporter/internal/fixtures"
)

func TestForVersionRewritesNewerFields(t *testing.T) {
	newer := fixtures.MGBAState(0x0B)
	got, err := ForVersion(newer, 7)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := Version(got); v != 7 {
		t.Fatalf("version %d", v)
	}
	for _, f := range fieldsSince {
		if !bytes.Equal(got[f.from:f.to], make([]byte, f.to-f.from)) {
			t.Errorf("bytes %#x-%#x kept", f.from, f.to)
		}
	}
	// Everything else, extra data and the GPIO bytes v7 masks off included, stays as it was.
	kept := bytes.Clone(newer)
	copy(kept, got[:4])
	copy(kept[rtcTransferStep:], got[rtcTransferStep:rtcTransferStep+4])
	for _, f := range fieldsSince {
		clear(kept[f.from:f.to])
	}
	if !bytes.Equal(got, kept) {
		t.Fatal("bytes outside the newer fields changed")
	}
	if binary.LittleEndian.Uint32(newer) != stateMagic+0x0B {
		t.Fatal("the input was changed")
	}
}

func TestForVersionKeepsWhatANewerVersionReads(t *testing.T) {
	// The DMA latches a v9 loader doesn't read go; the clock's transfer step, which v9 keeps
	// elsewhere, stays as the newer state wrote it.
	newer := fixtures.MGBAState(0x0B)
	got, err := ForVersion(newer, 9)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got[0x3D4:0x3E8], make([]byte, 0x14)) {
		t.Fatal("DMA latches kept")
	}
	if !bytes.Equal(got[rtcTransferStep:rtcTransferStep+4], newer[rtcTransferStep:rtcTransferStep+4]) {
		t.Fatal("transfer step rewritten for a version that has the state machine")
	}
}

func TestForVersionDerivesRTCTransferStep(t *testing.T) {
	for pins, want := range map[byte]uint32{0x00: 0, 0x02: 0, 0x01: 1, 0x03: 1, 0x04: 2, 0x05: 2, 0x07: 2} {
		newer := fixtures.MGBAState(9)
		newer[pinState] = pins
		got, err := ForVersion(newer, 7)
		if err != nil {
			t.Fatal(err)
		}
		if step := binary.LittleEndian.Uint32(got[rtcTransferStep:]); step != want {
			t.Errorf("pins %#x: step %d, want %d", pins, step, want)
		}
	}
}

func TestForVersionLeavesOlderStates(t *testing.T) {
	old := fixtures.MGBAState(7)
	got, err := ForVersion(old, 9)
	if err != nil || !bytes.Equal(got, old) {
		t.Fatalf("an older state changed: %v", err)
	}
}

func TestForVersionRefusesUnknownVersions(t *testing.T) {
	if _, err := ForVersion(fixtures.MGBAState(0x0C), 7); !errors.Is(err, ErrTooNew) {
		t.Fatalf("got %v", err)
	}
}

func TestStateRejectsOtherData(t *testing.T) {
	gb := fixtures.MGBAState(7)
	binary.LittleEndian.PutUint32(gb, 0x00400002) // a Game Boy state
	for name, data := range map[string][]byte{"short": make([]byte, 100), "game boy": gb} {
		if _, err := State(data); !errors.Is(err, ErrNotState) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestStateFromPNG(t *testing.T) {
	raw := fixtures.MGBAState(0x0B)
	got, err := State(fixtures.MGBAPNG(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("the PNG's state didn't come out in the raw layout")
	}
}

func TestHeader(t *testing.T) {
	s := fixtures.MGBAState(7)
	title, code := Title(s)
	if ROMCRC(s) != fixtures.ROMCRC || title != fixtures.Title || code != fixtures.GameCode {
		t.Fatalf("crc %08x title %q code %q", ROMCRC(s), title, code)
	}
}
