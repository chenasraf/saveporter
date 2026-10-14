package saveporter

import (
	"path/filepath"
	"strconv"
	"strings"
)

// Slot is a save state slot: 0-9, or Auto for the state an emulator writes and loads by itself.
type Slot int

// Auto is the automatic state slot: RetroArch's ".state.auto", the mGBA app's slot 0.
const Auto Slot = -1

func (s Slot) String() string {
	if s == Auto {
		return "auto"
	}
	return strconv.Itoa(int(s))
}

// StateName is a state file's name taken apart.
type StateName struct {
	// Base is the name without the state extension, usually the ROM's name.
	Base string
	// Emulator is the emulator family the name belongs to: RetroArch for ".state" names (which
	// EmulatorJS shares), MGBA for ".ss0"-".ss9".
	Emulator Emulator
	Slot     Slot
}

// ParseStateName recognises a state file's name: RetroArch's "<base>.state", "<base>.state1"-"9"
// and "<base>.state.auto", and the mGBA app's "<base>.ss0"-"9". The directory is ignored.
func ParseStateName(name string) (StateName, bool) {
	name = filepath.Base(name)
	if base, ok := strings.CutSuffix(name, ".state.auto"); ok && base != "" {
		return StateName{Base: base, Emulator: RetroArch, Slot: Auto}, true
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	if base == "" {
		return StateName{}, false
	}
	switch {
	case ext == ".state":
		return StateName{Base: base, Emulator: RetroArch, Slot: 0}, true
	case len(ext) == len(".state1") && strings.HasPrefix(ext, ".state") && isDigit(ext[6]) && ext[6] != '0':
		return StateName{Base: base, Emulator: RetroArch, Slot: Slot(ext[6] - '0')}, true
	case len(ext) == len(".ss1") && strings.HasPrefix(ext, ".ss") && isDigit(ext[3]):
		// The mGBA app's slot 0 is its auto-state, loaded on launch.
		slot := Slot(ext[3] - '0')
		if slot == 0 {
			slot = Auto
		}
		return StateName{Base: base, Emulator: MGBA, Slot: slot}, true
	}
	return StateName{}, false
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// StateFileName is the name an emulator gives a state of base in slot. The mGBA app has no slot
// apart from its auto-state numbered 0, so slot 0 and Auto both name ".ss0" there.
func StateFileName(base string, e Emulator, slot Slot) string {
	switch e {
	case MGBA:
		if slot == Auto {
			slot = 0
		}
		return base + ".ss" + slot.String()
	default:
		switch slot {
		case Auto:
			return base + ".state.auto"
		case 0:
			return base + ".state"
		}
		return base + ".state" + slot.String()
	}
}

// TargetForName is the target a state written under name is for, when the name says.
//
// RetroArch and EmulatorJS share ".state" names and the container, so a ".state" name targets the
// newest state version both load: the file works in either.
func TargetForName(name string) (Target, bool) {
	n, ok := ParseStateName(name)
	if !ok {
		return Target{}, false
	}
	if n.Emulator == MGBA {
		return Target{Emulator: MGBA}, true
	}
	return Target{Emulator: EmulatorJS}, true
}
