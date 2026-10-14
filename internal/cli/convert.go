package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/chenasraf/saveporter"
)

// Result statuses.
const (
	statusConverted = "converted"
	statusWould     = "would convert"
	statusSkipped   = "skipped"
	statusFailed    = "failed"
)

// result is what happened to one input.
type result struct {
	Input  string           `json:"input"`
	Output string           `json:"output,omitempty"`
	Status string           `json:"status"`
	Reason string           `json:"reason,omitempty"`
	From   *saveporter.Info `json:"from,omitempty"`
	// To is the emulator written for, and ToVersion the core state version written.
	To        saveporter.Emulator `json:"to,omitempty"`
	ToVersion uint32              `json:"toVersion,omitempty"`
	// ToLayout is the Gambatte state layout written.
	ToLayout string `json:"toLayout,omitempty"`
}

// job is one input to convert to one output.
type job struct {
	input, output string
	target        saveporter.Target
	data          []byte
	info          saveporter.Info
	slot          saveporter.Slot
}

func runSingle(e env, o options, input, output string) error {
	target, ok, err := o.flagTarget()
	if err != nil {
		return err
	}
	if !ok {
		if output == stdioMarker {
			return usagef("writing to stdout needs --to")
		}
		named, known := saveporter.TargetForName(output)
		if !known {
			return usagef("can't tell the target from %q; pass --to (%s)", filepath.Base(output), emulatorList())
		}
		named.StateVersion = target.StateVersion
		target = named
	}
	data, err := readInput(e, input)
	if err != nil {
		return report(e, o, []result{{Input: input, Output: output, Status: statusFailed, Reason: err.Error()}})
	}
	r := convertOne(e, o, job{input: input, output: output, target: target, data: data})
	return report(e, o, []result{r})
}

func runBatch(e env, o options, args []string) error {
	target, ok, err := o.flagTarget()
	if err != nil {
		return err
	}
	if !ok {
		return usagef("a batch needs --to (%s)", emulatorList())
	}
	if slices.Contains(args, stdioMarker) {
		return usagef("a batch reads files, not stdin")
	}
	inputs, err := gatherInputs(args)
	if err != nil {
		return err
	}

	var results []result
	var jobs []job
	for _, in := range inputs {
		data, err := os.ReadFile(in)
		if err != nil {
			results = append(results, result{Input: in, Status: statusFailed, Reason: err.Error()})
			continue
		}
		info, err := saveporter.Detect(data)
		if err != nil {
			results = append(results, result{Input: in, Status: statusSkipped, Reason: "not a save state saveporter knows"})
			continue
		}
		base, slot := strings.TrimSuffix(filepath.Base(in), filepath.Ext(in)), saveporter.Slot(0)
		if n, ok := saveporter.ParseStateName(in); ok {
			base, slot = n.Base, n.Slot
		}
		out := filepath.Join(o.outDir, saveporter.StateFileName(base, target.Emulator, slot))
		jobs = append(jobs, job{input: in, output: out, target: target, data: data, info: info, slot: slot})
	}

	// Inputs that name the same output: the auto-state wins, then slot 0, then the first found.
	rank := func(s saveporter.Slot) int {
		switch s {
		case saveporter.Auto:
			return 0
		case 0:
			return 1
		}
		return 2
	}
	slices.SortStableFunc(jobs, func(a, b job) int { return rank(a.slot) - rank(b.slot) })
	taken := map[string]string{}
	var run []job
	for _, j := range jobs {
		if from, ok := taken[j.output]; ok {
			results = append(results, result{Input: j.input, Output: j.output, Status: statusSkipped, Reason: fmt.Sprintf("%s already comes from %s", filepath.Base(j.output), from)})
			continue
		}
		taken[j.output] = j.input
		run = append(run, j)
	}
	if !o.dryRun && len(run) > 0 {
		if err := os.MkdirAll(o.outDir, 0o755); err != nil {
			return err
		}
	}
	for _, j := range run {
		results = append(results, convertOne(e, o, j))
	}
	// Reported in the order the inputs were found.
	order := map[string]int{}
	for i, in := range inputs {
		order[in] = i
	}
	slices.SortStableFunc(results, func(a, b result) int { return order[a.Input] - order[b.Input] })
	return report(e, o, results)
}

// gatherInputs expands the arguments into files: files as given, directories searched for files
// named like states.
func gatherInputs(args []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, arg := range args {
		st, err := os.Stat(arg)
		if err != nil {
			return nil, usagef("%v", err)
		}
		if !st.IsDir() {
			add(arg)
			continue
		}
		err = filepath.WalkDir(arg, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type().IsRegular() {
				if _, ok := saveporter.ParseStateName(p); ok {
					add(p)
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func readInput(e env, input string) ([]byte, error) {
	if input == stdioMarker {
		return io.ReadAll(e.stdin)
	}
	return os.ReadFile(input)
}

// convertOne converts a job's input and writes it, unless the output exists or this is a dry run.
func convertOne(e env, o options, j job) result {
	r := result{Input: j.input, Output: j.output, To: j.target.Emulator}
	info, err := saveporter.Detect(j.data)
	if err != nil {
		r.Status, r.Reason = statusFailed, err.Error()
		return r
	}
	r.From = &info
	out, err := saveporter.Convert(j.data, j.target)
	if err != nil {
		r.Status, r.Reason = statusFailed, err.Error()
		return r
	}
	if to, err := saveporter.Detect(out); err == nil {
		r.ToVersion, r.ToLayout = to.StateVersion, to.Layout
	}
	if j.output != stdioMarker && !o.force {
		if _, err := os.Lstat(j.output); err == nil {
			r.Status, r.Reason = statusSkipped, "exists; use --force to overwrite"
			return r
		}
	}
	if o.dryRun {
		r.Status = statusWould
		return r
	}
	if j.output == stdioMarker {
		_, err = e.stdout.Write(out)
	} else {
		err = writeFile(j.output, out)
	}
	if err != nil {
		r.Status, r.Reason = statusFailed, err.Error()
		return r
	}
	r.Status = statusConverted
	return r
}

// writeFile writes data next to path and renames it into place, so an interrupted write never
// leaves a half-written state behind.
func writeFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".saveporter-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// report prints the results and returns errFailed when any failed.
func report(e env, o options, results []result) error {
	// The converted state may be on stdout, so the report goes to stderr then.
	w := e.stdout
	for _, r := range results {
		if r.Output == stdioMarker {
			w = e.stderr
		}
	}
	failed := slices.ContainsFunc(results, func(r result) bool { return r.Status == statusFailed })
	if o.json {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			return err
		}
	} else {
		printResults(w, e.stderr, o, results)
	}
	if failed {
		return errFailed
	}
	return nil
}

func printResults(w, errw io.Writer, o options, results []result) {
	if len(results) == 0 {
		fmt.Fprintln(errw, "no save states found")
		return
	}
	counts := map[string]int{}
	for _, r := range results {
		counts[r.Status]++
		if r.Status == statusFailed {
			fmt.Fprintf(errw, "%s: %s\n", r.Input, r.Reason)
			continue
		}
		if o.quiet {
			continue
		}
		switch r.Status {
		case statusSkipped:
			fmt.Fprintf(w, "%s → skipped: %s\n", r.Input, r.Reason)
		default:
			prefix := ""
			if r.Status == statusWould {
				prefix = "(dry run) "
			}
			fmt.Fprintf(w, "%s%s → %s\n", prefix, r.Input, r.Output)
			if r.From != nil {
				fmt.Fprintf(w, "  %s · %s state %s → %s · %s (%s) → %s\n",
					strings.ToUpper(string(r.From.System)), coreName(r.From.Core),
					stateVersion(r.From.StateVersion, r.From.Layout), stateVersion(r.ToVersion, r.ToLayout),
					writersName(r.From.Writers), r.From.Compression, emulatorName(r.To))
			}
		}
	}
	if len(results) > 1 && !o.quiet {
		var parts []string
		for _, s := range []string{statusConverted, statusWould, statusSkipped, statusFailed} {
			if counts[s] > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", counts[s], s))
			}
		}
		fmt.Fprintln(w, strings.Join(parts, ", "))
	}
}

func coreName(core string) string {
	switch core {
	case saveporter.CoreMGBA:
		return "mGBA"
	case saveporter.CoreGambatte:
		return "Gambatte"
	}
	return core
}

// stateVersion shows a state's layout: mGBA's version, or the optional parts a Gambatte state has.
func stateVersion(version uint32, layout string) string {
	if layout != "" {
		return layout
	}
	return fmt.Sprintf("v%d", version)
}
