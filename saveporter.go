// Package saveporter converts save states between emulators that run the same console core in
// different wrappers: RetroArch, EmulatorJS (the web player RomM and others embed) and standalone
// emulator apps.
//
// Detect says what a file is; Convert rewrites it for a target emulator. The formats themselves
// live in the subpackages (retroarch, mgba), for callers that need finer control.
package saveporter

import (
	_ "embed"
	"errors"
	"fmt"
	"strings"

	"github.com/chenasraf/saveporter/mgba"
	"github.com/chenasraf/saveporter/retroarch"
)

//go:embed version.txt
var version string

// Version is the version of saveporter.
var Version = strings.TrimSpace(version)

// Emulator is a program that reads and writes save states.
type Emulator string

const (
	RetroArch  Emulator = "retroarch"
	EmulatorJS Emulator = "emulatorjs"
	// MGBA is the mGBA app (as opposed to its libretro core, which RetroArch and EmulatorJS run).
	MGBA Emulator = "mgba"
)

// Emulators lists the emulators Convert can write for.
var Emulators = []Emulator{RetroArch, EmulatorJS, MGBA}

// System is a console.
type System string

const GBA System = "gba"

// Container is how a core's state is wrapped in a file.
type Container string

const (
	// ContainerRetroArch is RetroArch's RASTATE container, which EmulatorJS writes too.
	ContainerRetroArch Container = "retroarch"
	// ContainerMGBAPNG is the mGBA app's PNG screenshot with the state inside.
	ContainerMGBAPNG Container = "mgba-png"
	// ContainerRaw is the core's state alone.
	ContainerRaw Container = "raw"
)

// Info describes a save state file.
type Info struct {
	System System
	// Core is the emulator core whose state it is.
	Core string
	// StateVersion is the core's state layout version.
	StateVersion uint32
	Container    Container
	// Compression is how the file is compressed, "none" when it isn't.
	Compression string
	// Writers are the emulators that write files of this shape.
	Writers []Emulator
	// ROMCRC is the CRC32 of the ROM the state was saved from.
	ROMCRC uint32
	// Title and GameCode are the game's, from the cartridge header.
	Title, GameCode string
}

// ErrUnknownFormat means data isn't a save state saveporter knows.
var ErrUnknownFormat = errors.New("not a save state saveporter knows")

// Detect says what kind of save state data is.
func Detect(data []byte) (Info, error) {
	info := Info{Compression: retroarch.Uncompressed.String()}
	var core []byte
	switch {
	case mgba.IsPNG(data):
		info.Container, info.Writers = ContainerMGBAPNG, []Emulator{MGBA}
		core = data
	default:
		f, err := retroarch.Read(data)
		if err != nil {
			return Info{}, fmt.Errorf("%w: %w", ErrUnknownFormat, err)
		}
		core, info.Compression = f.Core, f.Compression.String()
		switch {
		case f.Compression != retroarch.Uncompressed:
			info.Container, info.Writers = ContainerRetroArch, []Emulator{RetroArch}
		case f.Container:
			info.Container, info.Writers = ContainerRetroArch, []Emulator{RetroArch, EmulatorJS}
		default:
			info.Container, info.Writers = ContainerRaw, []Emulator{MGBA, RetroArch}
		}
	}
	state, err := mgba.State(core)
	if err != nil {
		return Info{}, fmt.Errorf("%w: %w", ErrUnknownFormat, err)
	}
	info.System, info.Core = GBA, "mgba"
	info.StateVersion, _ = mgba.Version(state)
	info.ROMCRC = mgba.ROMCRC(state)
	info.Title, info.GameCode = mgba.Title(state)
	return info, nil
}

// Target is what Convert writes for.
type Target struct {
	Emulator Emulator
	// StateVersion caps the core's state layout version, for an emulator older or newer than the
	// one DefaultStateVersion assumes. Zero uses that default.
	StateVersion uint32
}

// DefaultStateVersion is the newest core state version an emulator is assumed to load, zero when
// states are passed on at their own version.
//
// EmulatorJS is assumed to be 4.2.3 and the mGBA app a 0.10 release. RetroArch's cores update
// with RetroArch and load older states, so states for it keep their version.
func DefaultStateVersion(e Emulator) uint32 {
	switch e {
	case EmulatorJS:
		return mgba.EmulatorJSVersion
	case MGBA:
		return mgba.AppVersion
	}
	return 0
}

// ErrUnknownEmulator means a Target names an emulator saveporter doesn't write for.
var ErrUnknownEmulator = errors.New("unknown emulator")

// Convert rewrites a save state for the target emulator.
func Convert(data []byte, to Target) ([]byte, error) {
	if _, err := Detect(data); err != nil {
		return nil, err
	}
	core := data
	if !mgba.IsPNG(data) {
		var err error
		if core, err = retroarch.CoreState(data); err != nil {
			return nil, err
		}
	}
	state, err := mgba.State(core)
	if err != nil {
		return nil, err
	}
	version := to.StateVersion
	if version == 0 {
		version = DefaultStateVersion(to.Emulator)
	}
	if version != 0 {
		if state, err = mgba.ForVersion(state, version); err != nil {
			return nil, err
		}
	}
	switch to.Emulator {
	case RetroArch, EmulatorJS:
		return retroarch.Wrap(state), nil
	case MGBA:
		return state, nil
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownEmulator, to.Emulator)
}
