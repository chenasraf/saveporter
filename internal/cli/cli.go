// Package cli is saveporter's command line.
package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/chenasraf/saveporter"
	"github.com/spf13/cobra"
)

// Exit codes.
const (
	exitOK      = 0
	exitFailed  = 1
	exitUsage   = 2
	stdioMarker = "-"
)

// usageError is a mistake in the command line, as opposed to a file that failed.
type usageError struct{ error }

func usagef(format string, args ...any) error { return usageError{fmt.Errorf(format, args...)} }

// errFailed means at least one file failed; what failed was already reported.
var errFailed = errors.New("some files failed")

type options struct {
	to           string
	stateVersion string
	outDir       string
	force        bool
	dryRun       bool
	quiet        bool
	json         bool
}

type env struct {
	stdin          io.Reader
	stdout, stderr io.Writer
}

// Run runs the command line with args (without the program name) and returns the exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	e := env{stdin: stdin, stdout: stdout, stderr: stderr}
	root := newRoot(e)
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.Execute()
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errFailed):
		return exitFailed
	}
	fmt.Fprintln(stderr, "saveporter:", err)
	var u usageError
	if errors.As(err, &u) {
		return exitUsage
	}
	return exitFailed
}

func flagError(_ *cobra.Command, err error) error { return usageError{err} }

func newRoot(e env) *cobra.Command {
	var o options
	root := &cobra.Command{
		Use:   "saveporter [flags] <input> <output>\n  saveporter [flags] --to <emulator> -o <dir> <input>...",
		Short: "Convert emulator save states between emulators",
		Long: `Convert emulator save states between emulators: RetroArch, EmulatorJS (the web player
RomM embeds) and the mGBA app.

The input's format is detected. The target comes from the output's name:
  .ss0-.ss9                       the mGBA app
  .state, .state1-9, .state.auto  RetroArch and EmulatorJS (a state both load)
or from --to, which is required for any other name and for batches.

With -o, every input (files, or directories searched for states) is converted into the
directory, named by slot: RetroArch's .state.auto and the mGBA app's .ss0 are each other's
auto-state, .stateN and .ssN the same numbered slot.`,
		Version:       saveporter.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if o.outDir != "" {
				return runBatch(e, o, args)
			}
			if len(args) != 2 {
				return usagef("expected <input> <output>, or -o <dir> with inputs; got %d arguments", len(args))
			}
			return runSingle(e, o, args[0], args[1])
		},
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.SetFlagErrorFunc(flagError)
	f := root.Flags()
	f.StringVarP(&o.to, "to", "t", "", "target emulator: "+emulatorList())
	f.StringVar(&o.stateVersion, "state-version", "", "core state version to write at most, or \"keep\" (default: the target's)")
	f.StringVarP(&o.outDir, "out-dir", "o", "", "convert every input into this directory")
	f.BoolVarP(&o.force, "force", "f", false, "overwrite existing outputs")
	f.BoolVarP(&o.dryRun, "dry-run", "n", false, "show what would be written, write nothing")
	f.BoolVarP(&o.quiet, "quiet", "q", false, "report errors only")
	f.BoolVar(&o.json, "json", false, "report as JSON")
	root.AddCommand(newInfo(e))
	return root
}

func emulatorList() string {
	names := make([]string, len(saveporter.Emulators))
	for i, em := range saveporter.Emulators {
		names[i] = string(em)
	}
	return strings.Join(names, ", ")
}

// flagTarget is the target the flags name, if --to is given.
func (o options) flagTarget() (saveporter.Target, bool, error) {
	var t saveporter.Target
	switch o.stateVersion {
	case "":
	case "keep":
		t.StateVersion = saveporter.KeepStateVersion
	default:
		v, err := strconv.ParseUint(o.stateVersion, 0, 32)
		if err != nil || v == 0 {
			return t, false, usagef("--state-version takes a positive number or \"keep\", not %q", o.stateVersion)
		}
		t.StateVersion = uint32(v)
	}
	if o.to == "" {
		return t, false, nil
	}
	for _, em := range saveporter.Emulators {
		if strings.EqualFold(o.to, string(em)) {
			t.Emulator = em
			return t, true, nil
		}
	}
	return t, false, usagef("unknown emulator %q for --to; one of %s", o.to, emulatorList())
}

// emulatorName is how an emulator is shown to people.
func emulatorName(em saveporter.Emulator) string {
	switch em {
	case saveporter.RetroArch:
		return "RetroArch"
	case saveporter.EmulatorJS:
		return "EmulatorJS"
	case saveporter.MGBA:
		return "mGBA app"
	}
	return string(em)
}

func writersName(ws []saveporter.Emulator) string {
	names := make([]string, len(ws))
	for i, w := range ws {
		names[i] = emulatorName(w)
	}
	return strings.Join(names, " or ")
}
