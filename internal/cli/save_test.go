package cli

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenasraf/saveporter/internal/fixtures"
)

const now = "1760400000"

// clockGame writes a ROM of an MBC3 cartridge with a clock and 32 KiB of RAM, and its Gambatte save.
func clockGame(t *testing.T, dir string) (rom, srm string, ram []byte) {
	t.Helper()
	rom = write(t, dir, "Game.gbc", fixtures.GBHeader(0x10, 0x06, 0x03, true))
	ram = bytes.Repeat([]byte{0x12, 0x34}, 0x4000)
	srm = write(t, dir, "Game.srm", ram)
	write(t, dir, "Game.rtc", binary.LittleEndian.AppendUint64(nil, 1760400000-3600))
	return rom, srm, ram
}

func TestSaveToMGBAApp(t *testing.T) {
	dir := t.TempDir()
	rom, srm, ram := clockGame(t, dir)
	out := filepath.Join(dir, "out", "Game.sav")
	if err := os.Mkdir(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	r := runCLI(t, nil, "save", "--rom", rom, "--from", "gambatte", "--to", "mgba-app", "--now", now, srm, out)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "Game.rtc") || !strings.Contains(r.stdout, "MBC3 · gambatte → mgba-app") {
		t.Fatalf("report: %s", r.stdout)
	}
	sav, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(sav) != len(ram)+48 || !bytes.Equal(sav[:len(ram)], ram) {
		t.Fatalf("%d bytes", len(sav))
	}
	// One hour on the clock, in the latched hours register.
	if h := binary.LittleEndian.Uint32(sav[len(ram)+28:]); h != 1 {
		t.Fatalf("hours %d", h)
	}

	// Back, with the format taken from the .sav, to the files it came from.
	back := filepath.Join(dir, "back.srm")
	r = runCLI(t, nil, "save", "--rom", rom, "--to", "gambatte", "--now", now, out, back)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	for _, x := range []string{".srm", ".rtc"} {
		a, _ := os.ReadFile(filepath.Join(dir, "Game"+x))
		b, _ := os.ReadFile(filepath.Join(dir, "back"+x))
		if !bytes.Equal(a, b) {
			t.Errorf("%s changed through the round trip", x)
		}
	}
}

func TestSaveRomInZip(t *testing.T) {
	dir := t.TempDir()
	_, srm, _ := clockGame(t, dir)
	var z bytes.Buffer
	w := zip.NewWriter(&z)
	f, _ := w.Create("readme.txt")
	_, _ = f.Write([]byte("hi"))
	f, _ = w.Create("Game.gbc")
	_, _ = f.Write(fixtures.GBHeader(0x10, 0x06, 0x03, true))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	rom := write(t, dir, "Game.zip", z.Bytes())
	r := runCLI(t, nil, "save", "--rom", rom, "--from", "gambatte", "--to", "sameboy", srm, filepath.Join(dir, "sb.srm"))
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if rtc, err := os.ReadFile(filepath.Join(dir, "sb.rtc")); err != nil || len(rtc) != 32 {
		t.Fatalf("SameBoy's clock: %d bytes, %v", len(rtc), err)
	}
}

func TestSaveNeverOverwritesWithoutForce(t *testing.T) {
	dir := t.TempDir()
	rom, srm, _ := clockGame(t, dir)
	out := write(t, dir, "Game.sav", []byte("mine"))
	r := runCLI(t, nil, "save", "--rom", rom, "--from", "gambatte", "--to", "mgba-app", srm, out)
	if r.code != 1 {
		t.Fatalf("exit %d", r.code)
	}
	if b, _ := os.ReadFile(out); string(b) != "mine" {
		t.Fatal("overwritten")
	}
	r = runCLI(t, nil, "save", "-n", "--rom", rom, "--from", "gambatte", "--to", "mgba-app", srm, filepath.Join(dir, "new.sav"))
	if _, err := os.Stat(filepath.Join(dir, "new.sav")); r.code != 0 || err == nil {
		t.Fatalf("dry run: exit %d, wrote: %v", r.code, err == nil)
	}
}

func TestSaveUsage(t *testing.T) {
	dir := t.TempDir()
	rom, srm, _ := clockGame(t, dir)
	for name, args := range map[string][]string{
		"no to":        {"save", "--rom", rom, "--from", "gambatte", srm, "x.srm"},
		"unknown to":   {"save", "--rom", rom, "--from", "gambatte", "--to", "snes9x", srm, "x.srm"},
		"no from":      {"save", "--rom", rom, "--to", "mgba", srm, "x.srm"},
		"bad now":      {"save", "--rom", rom, "--from", "gambatte", "--to", "mgba", "--now", "soon", srm, "x.srm"},
		"one argument": {"save", "--rom", rom, "--from", "gambatte", "--to", "mgba", srm},
	} {
		if r := runCLI(t, nil, args...); r.code != 2 {
			t.Errorf("%s: exit %d: %s", name, r.code, r.stderr)
		}
	}
}

func TestSaveWithoutROM(t *testing.T) {
	dir := t.TempDir()
	// Gambatte's clock is an MBC3's or a HuC3's: the ROM says which.
	_, srm, _ := clockGame(t, dir)
	r := runCLI(t, nil, "save", "--from", "gambatte", "--to", "mgba-app", srm, filepath.Join(dir, "x.sav"))
	if r.code != 1 || !strings.Contains(r.stderr, "pass --rom") {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	// 32 KiB of RAM and no clock is the same in either.
	plain := write(t, dir, "Plain.srm", bytes.Repeat([]byte{1, 2, 3}, 0x8000/3+1)[:0x8000])
	r = runCLI(t, nil, "save", "--from", "gambatte", "--to", "mgba-app", plain, filepath.Join(dir, "Plain.sav"))
	if r.code != 0 || !strings.Contains(r.stdout, "gambatte → mgba-app") {
		t.Fatalf("exit %d: %s%s", r.code, r.stdout, r.stderr)
	}
	if sav, err := os.ReadFile(filepath.Join(dir, "Plain.sav")); err != nil || len(sav) != 0x8000 {
		t.Fatalf("%d bytes, %v", len(sav), err)
	}
}

func TestSaveUnsupportedCart(t *testing.T) {
	dir := t.TempDir()
	rom := write(t, dir, "Kirby.gbc", fixtures.GBHeader(0x22, 0x05, 0x00, true))
	srm := write(t, dir, "Kirby.srm", make([]byte, 0x100))
	r := runCLI(t, nil, "save", "--rom", rom, "--from", "mgba", "--to", "gambatte", srm, filepath.Join(dir, "g.srm"))
	if r.code != 1 || !strings.Contains(r.stderr, "keeps no save") {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
}
