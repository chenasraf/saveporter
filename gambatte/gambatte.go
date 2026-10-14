// Package gambatte reads and rewrites the Gambatte core's Game Boy and Game Boy Color save states.
//
// A Gambatte state is a list of labelled records: two header bytes, a screenshot (empty in the
// libretro core), then every field as its label, a NUL, a 24-bit big-endian length and the value,
// sorted by label. The loader skips labels it doesn't know, but the libretro core refuses a state
// whose size isn't exactly its own. Builds differ only in optional records: the network link
// (RetroArch's builds have it, EmulatorJS's don't) and, from 2026-05, the Sachen mapper. So a state
// carries over between builds by adding or dropping those records.
package gambatte

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Layout is the optional records a Gambatte build writes.
type Layout struct {
	// NetLink is the network link cable's two records (netsfc, netsv).
	NetLink bool
	// Sachen is the Sachen MMC1 mapper's two records (schnlct, schnomk).
	Sachen bool
}

var (
	// RetroArchLayout is RetroArch's Gambatte core as the libretro buildbot builds it in 2025
	// (v0.5.0-netlink, b752252): with the network link.
	RetroArchLayout = Layout{NetLink: true}
	// EmulatorJSLayout is EmulatorJS 4.2.3's Gambatte core (the release RomM 5.x ships), built
	// without the network link.
	EmulatorJSLayout = Layout{}
)

func (l Layout) String() string {
	var parts []string
	if l.NetLink {
		parts = append(parts, "network link")
	}
	if l.Sachen {
		parts = append(parts, "Sachen")
	}
	if len(parts) == 0 {
		return "no optional parts"
	}
	return "with " + strings.Join(parts, " and ")
}

// Record is one labelled field of a state.
type Record struct {
	Label string
	Value []byte
}

// State is a parsed Gambatte state.
type State struct {
	// Header is the two bytes before the screenshot.
	Header [2]byte
	// Screenshot is the screenshot record's payload, empty from the libretro core.
	Screenshot []byte
	Records    []Record
}

// ErrNotState means data isn't a Gambatte save state.
var ErrNotState = errors.New("not a Gambatte save state")

// Every Gambatte state has these; another core's state that happens to parse as labelled records
// won't.
var requiredLabels = []string{"pc", "sp", "vram", "wram", "hram", "sram"}

// optional are the records each optional part of a layout adds, with the value its build writes
// in a state that never used it.
var optional = map[string]struct {
	in    func(Layout) bool
	value []byte
}{
	// The link's defaults: no serial transfer pending (netsfc), 0xFF on the line (netsv).
	"netsfc": {func(l Layout) bool { return l.NetLink }, []byte{0x00}},
	"netsv":  {func(l Layout) bool { return l.NetLink }, []byte{0xFF}},
	// A cartridge that isn't Sachen's never sets these.
	"schnlct": {func(l Layout) bool { return l.Sachen }, []byte{0x00}},
	"schnomk": {func(l Layout) bool { return l.Sachen }, []byte{0x00}},
}

const (
	lengthSize = 3
	maxLabel   = 64
)

// Parse reads a raw Gambatte state, as the libretro core serializes it.
func Parse(data []byte) (State, error) {
	var s State
	if len(data) < 2+lengthSize || data[0] != 0 {
		return s, ErrNotState
	}
	copy(s.Header[:], data)
	shot := int(u24(data[2:]))
	if 2+lengthSize+shot > len(data) {
		return s, fmt.Errorf("%w: screenshot runs past the end", ErrNotState)
	}
	s.Screenshot = data[2+lengthSize : 2+lengthSize+shot]
	for rest := data[2+lengthSize+shot:]; len(rest) > 0; {
		end := bytes.IndexByte(rest[:min(len(rest), maxLabel+1)], 0)
		if end <= 0 || end+1+lengthSize > len(rest) {
			return s, fmt.Errorf("%w: bad label", ErrNotState)
		}
		label := string(rest[:end])
		for _, c := range label {
			if c < 0x21 || c > 0x7E {
				return s, fmt.Errorf("%w: bad label", ErrNotState)
			}
		}
		n := int(u24(rest[end+1:]))
		rest = rest[end+1+lengthSize:]
		if n > len(rest) {
			return s, fmt.Errorf("%w: %s runs past the end", ErrNotState, label)
		}
		s.Records = append(s.Records, Record{label, rest[:n]})
		rest = rest[n:]
	}
	for _, l := range requiredLabels {
		if s.Value(l) == nil {
			return s, fmt.Errorf("%w: no %s", ErrNotState, l)
		}
	}
	return s, nil
}

func u24(b []byte) uint32 { return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2]) }

// Value is the value of the record with the label, nil if the state has none.
func (s State) Value(label string) []byte {
	for _, r := range s.Records {
		if r.Label == label {
			return r.Value
		}
	}
	return nil
}

// Layout is the optional records the state has.
func (s State) Layout() Layout {
	return Layout{NetLink: s.Value("netsv") != nil, Sachen: s.Value("schnomk") != nil}
}

// CGB reports whether the state is of a Game Boy Color: its work RAM is the Color's 32 KiB.
func (s State) CGB() bool { return len(s.Value("wram")) == 0x8000 }

// For returns the state with the optional records of the layout and no others, in label order.
func (s State) For(l Layout) State {
	out := s
	out.Records = nil
	for _, r := range s.Records {
		if o, ok := optional[r.Label]; ok && !o.in(l) {
			continue
		}
		out.Records = append(out.Records, r)
	}
	for label, o := range optional {
		if o.in(l) && s.Value(label) == nil {
			out.Records = append(out.Records, Record{label, slices.Clone(o.value)})
		}
	}
	// Gambatte writes in strcmp order and reads in it: a record out of place is skipped.
	slices.SortStableFunc(out.Records, func(a, b Record) int { return bytes.Compare([]byte(a.Label), []byte(b.Label)) })
	return out
}

// Bytes serializes the state.
func (s State) Bytes() []byte {
	b := append([]byte{}, s.Header[:]...)
	b = appendU24(b, len(s.Screenshot))
	b = append(b, s.Screenshot...)
	for _, r := range s.Records {
		b = append(b, r.Label...)
		b = append(b, 0)
		b = appendU24(b, len(r.Value))
		b = append(b, r.Value...)
	}
	return b
}

func appendU24(b []byte, n int) []byte { return append(b, byte(n>>16), byte(n>>8), byte(n)) }
