package saveporter

import "testing"

func TestParseStateName(t *testing.T) {
	for name, want := range map[string]StateName{
		"Golden Sun.state":            {Base: "Golden Sun", Emulator: RetroArch, Slot: 0},
		"Golden Sun.state3":           {Base: "Golden Sun", Emulator: RetroArch, Slot: 3},
		"Golden Sun.state.auto":       {Base: "Golden Sun", Emulator: RetroArch, Slot: Auto},
		"dir/Fire Emblem (U).ss0":     {Base: "Fire Emblem (U)", Emulator: MGBA, Slot: Auto},
		"Fire Emblem (U).ss7":         {Base: "Fire Emblem (U)", Emulator: MGBA, Slot: 7},
		"Pokemon - Fire Red.v2.state": {Base: "Pokemon - Fire Red.v2", Emulator: RetroArch, Slot: 0},
	} {
		got, ok := ParseStateName(name)
		if !ok || got != want {
			t.Errorf("%s: %+v, %v", name, got, ok)
		}
	}
	for _, name := range []string{"Golden Sun.srm", "Golden Sun.state.png", "Golden Sun.state0", "Golden Sun.state10", ".state", "Golden Sun.ss", "Golden Sun.ssx"} {
		if got, ok := ParseStateName(name); ok {
			t.Errorf("%s parsed as %+v", name, got)
		}
	}
}

func TestStateFileName(t *testing.T) {
	for _, c := range []struct {
		e    Emulator
		slot Slot
		want string
	}{
		{RetroArch, Auto, "G.state.auto"},
		{RetroArch, 0, "G.state"},
		{EmulatorJS, 4, "G.state4"},
		{MGBA, Auto, "G.ss0"},
		{MGBA, 0, "G.ss0"},
		{MGBA, 9, "G.ss9"},
	} {
		if got := StateFileName("G", c.e, c.slot); got != c.want {
			t.Errorf("%s slot %s: %s, want %s", c.e, c.slot, got, c.want)
		}
	}
}

func TestTargetForName(t *testing.T) {
	for name, want := range map[string]Emulator{"a.ss1": MGBA, "a.state": EmulatorJS, "a.state.auto": EmulatorJS, "a.state2": EmulatorJS} {
		got, ok := TargetForName(name)
		if !ok || got.Emulator != want {
			t.Errorf("%s: %v, %v", name, got, ok)
		}
	}
	if _, ok := TargetForName("a.bin"); ok {
		t.Error("a.bin has a target")
	}
}
