package saveporter

import (
	"bytes"
	"errors"
	"slices"
	"testing"

	"github.com/chenasraf/saveporter/internal/fixtures"
	"github.com/chenasraf/saveporter/mgba"
	"github.com/chenasraf/saveporter/retroarch"
)

func TestDetect(t *testing.T) {
	state := fixtures.MGBAState(9)
	for name, c := range map[string]struct {
		file        []byte
		container   Container
		compression string
		writers     []Emulator
	}{
		"RetroArch, compressed": {fixtures.RZIP(t, 1, retroarch.Wrap(state)), ContainerRetroArch, "rzip-deflate", []Emulator{RetroArch}},
		"RetroArch or web":      {retroarch.Wrap(state), ContainerRetroArch, "none", []Emulator{RetroArch, EmulatorJS}},
		"mGBA app PNG":          {fixtures.MGBAPNG(t, state), ContainerMGBAPNG, "none", []Emulator{MGBA}},
		"raw":                   {state, ContainerRaw, "none", []Emulator{MGBA, RetroArch}},
	} {
		info, err := Detect(c.file)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if info.System != GBA || info.Core != "mgba" || info.StateVersion != 9 || info.ROMCRC != fixtures.ROMCRC ||
			info.Title != fixtures.Title || info.GameCode != fixtures.GameCode {
			t.Errorf("%s: %+v", name, info)
		}
		if info.Container != c.container || info.Compression != c.compression || !slices.Equal(info.Writers, c.writers) {
			t.Errorf("%s: container %s compression %s writers %v", name, info.Container, info.Compression, info.Writers)
		}
	}
	if _, err := Detect([]byte("hello")); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("text: %v", err)
	}
}

func TestConvert(t *testing.T) {
	deck := fixtures.RZIP(t, 1, retroarch.Wrap(fixtures.MGBAState(9)))
	web := retroarch.Wrap(fixtures.MGBAState(7))
	for name, c := range map[string]struct {
		in      []byte
		to      Target
		version uint32
		wrapped bool
	}{
		"RetroArch to EmulatorJS":      {deck, Target{Emulator: EmulatorJS}, 7, true},
		"RetroArch to the mGBA app":    {deck, Target{Emulator: MGBA}, 7, false},
		"RetroArch to RetroArch":       {deck, Target{Emulator: RetroArch}, 9, true},
		"pinned version":               {deck, Target{Emulator: RetroArch, StateVersion: 8}, 8, true},
		"PNG to EmulatorJS":            {fixtures.MGBAPNG(t, fixtures.MGBAState(0x0B)), Target{Emulator: EmulatorJS}, 7, true},
		"EmulatorJS to RetroArch":      {web, Target{Emulator: RetroArch}, 7, true},
		"newer EmulatorJS, by version": {deck, Target{Emulator: EmulatorJS, StateVersion: 9}, 9, true},
	} {
		out, err := Convert(c.in, c.to)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if bytes.HasPrefix(out, []byte("RASTATE")) != c.wrapped {
			t.Errorf("%s: wrapped = %v", name, !c.wrapped)
		}
		info, err := Detect(out)
		if err != nil || info.StateVersion != c.version || info.ROMCRC != fixtures.ROMCRC {
			t.Errorf("%s: version %d, %v", name, info.StateVersion, err)
		}
	}
	// A web player file goes to RetroArch, and back, as is.
	if out, _ := Convert(web, Target{Emulator: RetroArch}); !bytes.Equal(out, web) {
		t.Error("EmulatorJS to RetroArch changed the file")
	}
	if out, _ := Convert(web, Target{Emulator: EmulatorJS}); !bytes.Equal(out, web) {
		t.Error("EmulatorJS to EmulatorJS changed the file")
	}
	if _, err := Convert(deck, Target{Emulator: "snes9x"}); !errors.Is(err, ErrUnknownEmulator) {
		t.Errorf("unknown emulator: %v", err)
	}
	if _, err := Convert(fixtures.MGBAState(0x0C), Target{Emulator: EmulatorJS}); !errors.Is(err, mgba.ErrTooNew) {
		t.Errorf("too new: %v", err)
	}
}

// Files come from other devices and servers, so damaged ones must fail, not panic.
func FuzzConvert(f *testing.F) {
	state := fixtures.MGBAState(9)
	prefix := func(b []byte, n int) []byte { return b[:min(n, len(b))] }
	f.Add(prefix(retroarch.Wrap(state), 4096))
	f.Add(prefix(fixtures.RZIP(f, 1, retroarch.Wrap(state)), 4096))
	f.Add(prefix(fixtures.RZIP(f, 2, retroarch.Wrap(state)), 4096))
	f.Add(prefix(fixtures.MGBAPNG(f, state), 4096))
	f.Add(prefix(retroarch.Wrap(fixtures.GambatteState(true, false)), 4096))
	f.Add(prefix(fixtures.MGBAGBState(3, true), 4096))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Detect(data)
		for _, e := range Emulators {
			out, err := Convert(data, Target{Emulator: e})
			if err != nil {
				continue
			}
			if _, err := Detect(out); err != nil {
				t.Fatalf("converted to something Detect refuses: %v", err)
			}
		}
	})
}
