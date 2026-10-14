package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chenasraf/saveporter"
	"github.com/chenasraf/saveporter/internal/fixtures"
	"github.com/chenasraf/saveporter/retroarch"
)

type run struct {
	code           int
	stdout, stderr string
}

func runCLI(t *testing.T, stdin []byte, args ...string) run {
	t.Helper()
	var out, errw bytes.Buffer
	code := Run(args, bytes.NewReader(stdin), &out, &errw)
	return run{code, out.String(), errw.String()}
}

// deckState is a compressed RetroArch state at v9, as a current RetroArch core writes it.
func deckState(t *testing.T) []byte {
	return fixtures.RZIP(t, 1, retroarch.Wrap(fixtures.MGBAState(9)))
}

func write(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func detect(t *testing.T, path string) saveporter.Info {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := saveporter.Detect(data)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestConvertTargetFromName(t *testing.T) {
	dir := t.TempDir()
	in := write(t, dir, "Game.state", deckState(t))
	for out, want := range map[string]saveporter.Container{
		"Game.ss1":        saveporter.ContainerRaw,
		"web/Game.state":  saveporter.ContainerRetroArch,
		"Game.state.auto": saveporter.ContainerRetroArch,
	} {
		path := filepath.Join(dir, out)
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		r := runCLI(t, nil, in, path)
		if r.code != 0 {
			t.Fatalf("%s: %+v", out, r)
		}
		info := detect(t, path)
		if info.Container != want || info.StateVersion != 7 {
			t.Errorf("%s: %s v%d", out, info.Container, info.StateVersion)
		}
	}
}

func TestConvertFlags(t *testing.T) {
	dir := t.TempDir()
	in := write(t, dir, "Game.state", deckState(t))
	out := filepath.Join(dir, "out.bin")
	if r := runCLI(t, nil, "--to", "retroarch", in, out); r.code != 0 {
		t.Fatal(r)
	}
	if v := detect(t, out).StateVersion; v != 9 {
		t.Errorf("--to retroarch wrote v%d", v)
	}
	if r := runCLI(t, nil, "-f", "--to", "mgba", "--state-version", "keep", in, out); r.code != 0 {
		t.Fatal(r)
	}
	if v := detect(t, out).StateVersion; v != 9 {
		t.Errorf("--state-version keep wrote v%d", v)
	}
	if r := runCLI(t, nil, "-f", "--state-version", "8", in, filepath.Join(dir, "Game.state1")); r.code != 0 {
		t.Fatal(r)
	}
	if v := detect(t, filepath.Join(dir, "Game.state1")).StateVersion; v != 8 {
		t.Errorf("--state-version 8 wrote v%d", v)
	}
}

func TestConvertNeverOverwritesWithoutForce(t *testing.T) {
	dir := t.TempDir()
	in := write(t, dir, "Game.state", deckState(t))
	out := write(t, dir, "Game.ss0", []byte("precious"))
	r := runCLI(t, nil, in, out)
	if r.code != 0 || !strings.Contains(r.stdout, "exists") {
		t.Fatalf("%+v", r)
	}
	if data, _ := os.ReadFile(out); string(data) != "precious" {
		t.Fatal("overwritten without --force")
	}
	if r := runCLI(t, nil, "--force", in, out); r.code != 0 {
		t.Fatal(r)
	}
	if detect(t, out).StateVersion != 7 {
		t.Fatal("--force didn't write")
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	in := write(t, dir, "Game.state", deckState(t))
	out := filepath.Join(dir, "Game.ss0")
	r := runCLI(t, nil, "-n", in, out)
	if r.code != 0 || !strings.Contains(r.stdout, "dry run") {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("dry run wrote the output")
	}
}

func TestUsageErrors(t *testing.T) {
	dir := t.TempDir()
	in := write(t, dir, "Game.state", deckState(t))
	for name, args := range map[string][]string{
		"unknown output name": {in, filepath.Join(dir, "out.bin")},
		"stdout without --to": {in, "-"},
		"unknown emulator":    {"--to", "snes9x", in, filepath.Join(dir, "x.ss0")},
		"bad version":         {"--state-version", "seven", in, filepath.Join(dir, "x.ss0")},
		"one argument":        {in},
		"batch without --to":  {"-o", dir, in},
		"unknown flag":        {"--bogus", in, filepath.Join(dir, "x.ss0")},
	} {
		if r := runCLI(t, nil, args...); r.code != exitUsage {
			t.Errorf("%s: exit %d %q", name, r.code, r.stderr)
		}
	}
}

func TestFailuresExitOne(t *testing.T) {
	dir := t.TempDir()
	notState := write(t, dir, "notes.state", []byte("not a state"))
	if r := runCLI(t, nil, notState, filepath.Join(dir, "x.ss0")); r.code != exitFailed {
		t.Errorf("not a state: exit %d", r.code)
	}
	if r := runCLI(t, nil, filepath.Join(dir, "missing.state"), filepath.Join(dir, "x.ss0")); r.code != exitFailed {
		t.Errorf("missing input: exit %d", r.code)
	}
}

func TestStdio(t *testing.T) {
	r := runCLI(t, deckState(t), "--to", "mgba", "-", "-")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	info, err := saveporter.Detect([]byte(r.stdout))
	if err != nil || info.Container != saveporter.ContainerRaw || info.StateVersion != 7 {
		t.Fatalf("%+v %v", info, err)
	}
	if !strings.Contains(r.stderr, "→ -") {
		t.Errorf("report not on stderr: %q", r.stderr)
	}
}

func TestBatch(t *testing.T) {
	src, out := t.TempDir(), t.TempDir()
	state := deckState(t)
	write(t, src, "Golden Sun.state", state)
	write(t, src, "Golden Sun.state.auto", state)
	write(t, src, "Golden Sun.state2", state)
	write(t, src, "nested/Fire Emblem.state", state)
	write(t, src, "Chrono Trigger.state", []byte("an SNES state"))
	write(t, src, "Golden Sun.state.png", []byte("a thumbnail"))
	write(t, out, "Fire Emblem.ss0", []byte("precious"))

	r := runCLI(t, nil, "--json", "--to", "mgba", "-o", out, src)
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	var results []result
	if err := json.Unmarshal([]byte(r.stdout), &results); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, res := range results {
		got[filepath.Base(res.Input)] = res.Status + " " + filepath.Base(res.Output)
	}
	want := map[string]string{
		"Golden Sun.state.auto": "converted Golden Sun.ss0",
		"Golden Sun.state":      "skipped Golden Sun.ss0",
		"Golden Sun.state2":     "converted Golden Sun.ss2",
		"Fire Emblem.state":     "skipped Fire Emblem.ss0",
		"Chrono Trigger.state":  "skipped .",
	}
	for in, w := range want {
		if got[in] != w {
			t.Errorf("%s: %q, want %q", in, got[in], w)
		}
	}
	if _, ok := got["Golden Sun.state.png"]; ok {
		t.Error("a thumbnail was taken as an input")
	}
	if data, _ := os.ReadFile(filepath.Join(out, "Fire Emblem.ss0")); string(data) != "precious" {
		t.Error("an existing output was overwritten")
	}
	if detect(t, filepath.Join(out, "Golden Sun.ss2")).StateVersion != 7 {
		t.Error("batch output isn't for the mGBA app")
	}
}

func TestBatchToRetroArchKeepsSlots(t *testing.T) {
	src, out := t.TempDir(), t.TempDir()
	raw, err := saveporter.Convert(deckState(t), saveporter.Target{Emulator: saveporter.MGBA})
	if err != nil {
		t.Fatal(err)
	}
	write(t, src, "Game.ss0", raw)
	write(t, src, "Game.ss3", raw)
	if r := runCLI(t, nil, "--to", "retroarch", "-o", out, src); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	for _, name := range []string{"Game.state.auto", "Game.state3"} {
		if detect(t, filepath.Join(out, name)).Container != saveporter.ContainerRetroArch {
			t.Errorf("%s not written", name)
		}
	}
}

func TestInfo(t *testing.T) {
	dir := t.TempDir()
	in := write(t, dir, "Game.state", deckState(t))
	bad := write(t, dir, "bad.state", []byte("nope"))
	r := runCLI(t, nil, "info", in)
	if r.code != 0 || !strings.Contains(r.stdout, "version 9") || !strings.Contains(r.stdout, "rzip-deflate") ||
		!strings.Contains(r.stdout, fixtures.Title) {
		t.Fatalf("%+v", r)
	}
	r = runCLI(t, nil, "info", "--json", in, bad)
	if r.code != exitFailed {
		t.Fatalf("exit %d", r.code)
	}
	var results []infoResult
	if err := json.Unmarshal([]byte(r.stdout), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Info == nil || results[0].Info.ROMCRC != fixtures.ROMCRC || results[1].Error == "" {
		t.Fatalf("%+v", results)
	}
}

func TestVersion(t *testing.T) {
	if r := runCLI(t, nil, "--version"); r.code != 0 || strings.TrimSpace(r.stdout) != saveporter.Version {
		t.Fatalf("%+v", r)
	}
}
