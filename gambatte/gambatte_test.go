package gambatte

import (
	"bytes"
	"errors"
	"testing"

	"github.com/chenasraf/saveporter/internal/fixtures"
)

func TestParseAndBytes(t *testing.T) {
	data := fixtures.GambatteState(true, false)
	s, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(s.Bytes(), data) {
		t.Fatal("serializing the parsed state changed it")
	}
	if l := s.Layout(); l != RetroArchLayout {
		t.Fatalf("layout %+v", l)
	}
	if !s.CGB() {
		t.Fatal("a 32 KiB work RAM is a Game Boy Color's")
	}
}

func TestForLayout(t *testing.T) {
	deck := fixtures.GambatteState(true, false)
	web := fixtures.GambatteState(false, false)
	s, _ := Parse(deck)
	if got := s.For(EmulatorJSLayout).Bytes(); !bytes.Equal(got, web) {
		t.Fatalf("network link records not dropped: %d bytes, want %d", len(got), len(web))
	}
	w, _ := Parse(web)
	if got := w.For(RetroArchLayout).Bytes(); !bytes.Equal(got, deck) {
		t.Fatal("network link records not added at their defaults, in label order")
	}
	sachen := fixtures.GambatteState(true, true)
	if got := s.For(Layout{NetLink: true, Sachen: true}).Bytes(); !bytes.Equal(got, sachen) {
		t.Fatal("Sachen records not added")
	}
	m, _ := Parse(sachen)
	if got := m.For(RetroArchLayout).Bytes(); !bytes.Equal(got, deck) {
		t.Fatal("Sachen records not dropped")
	}
}

func TestParseRejects(t *testing.T) {
	good := fixtures.GambatteState(false, false)
	tests := map[string][]byte{
		"empty":         nil,
		"truncated":     good[:len(good)-1],
		"mGBA magic":    append([]byte{0x03, 0x00, 0x40, 0x00}, good[4:]...),
		"no work RAM":   bytes.Replace(good, []byte("wram\x00"), []byte("wrax\x00"), 1),
		"label control": bytes.Replace(good, []byte("pc\x00"), []byte("p\x01\x00"), 1),
	}
	for name, data := range tests {
		if _, err := Parse(data); !errors.Is(err, ErrNotState) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func FuzzParse(f *testing.F) {
	f.Add(fixtures.GambatteState(true, false)[:512])
	f.Add(fixtures.GambatteState(false, true)[:512])
	f.Fuzz(func(t *testing.T, data []byte) {
		s, err := Parse(data)
		if err != nil {
			return
		}
		if !bytes.Equal(s.Bytes(), data) {
			t.Fatal("a parsed state doesn't serialize back to itself")
		}
		_ = s.For(RetroArchLayout).Bytes()
	})
}
