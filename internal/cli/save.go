package cli

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chenasraf/saveporter"
	"github.com/chenasraf/saveporter/gb"
	"github.com/spf13/cobra"
)

type saveOptions struct {
	rom, from, to, now string
	force, dryRun      bool
}

func newSave(e env) *cobra.Command {
	var o saveOptions
	cmd := &cobra.Command{
		Use:   "save --rom <rom> --to <format> [--from <format>] <input> <output>",
		Short: "Convert Game Boy and Game Boy Color battery saves between emulators",
		Long: `Convert a Game Boy or Game Boy Color battery save between emulators.

The input is a ".srm", read with the ".rtc" beside it when there is one, or the mGBA app's
".sav". The output's name without its extension names the files written: "<name>.srm" and
"<name>.rtc", or "<name>.sav" for the mGBA app.

--rom is the game's ROM (or a zip of it): its header says what the save holds.

Formats are the emulator cores:
  gambatte   RetroArch's and EmulatorJS's Gambatte core
  sameboy    RetroArch's SameBoy core
  vbam       RetroArch's VBA-M core
  gearboy    RetroArch's Gearboy core
  mgba       RetroArch's and EmulatorJS's mGBA core
  mgba-app   the mGBA app
EmulatorJS keeps no ".rtc": use its core's format and leave the clock out.`,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 2 {
				return usagef("expected <input> <output>; got %d arguments", len(args))
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error { return runSave(e, o, args[0], args[1]) },
	}
	cmd.SetFlagErrorFunc(flagError)
	f := cmd.Flags()
	f.StringVar(&o.rom, "rom", "", "the game's ROM, or a zip of it (required)")
	f.StringVar(&o.from, "from", "", "the input's format (default: mgba-app for a .sav)")
	f.StringVarP(&o.to, "to", "t", "", "the output's format (required): "+formatList())
	f.StringVar(&o.now, "now", "", "carry the clock over at this unix time (default: now)")
	f.BoolVarP(&o.force, "force", "f", false, "overwrite existing outputs")
	f.BoolVarP(&o.dryRun, "dry-run", "n", false, "show what would be written, write nothing")
	return cmd
}

func formatList() string {
	names := make([]string, len(gb.Formats))
	for i, f := range gb.Formats {
		names[i] = string(f)
	}
	return strings.Join(names, ", ")
}

func parseFormat(flag, s string) (gb.Format, error) {
	for _, f := range gb.Formats {
		if strings.EqualFold(s, string(f)) {
			return f, nil
		}
	}
	return "", usagef("unknown format %q for --%s; one of %s", s, flag, formatList())
}

func runSave(e env, o saveOptions, input, output string) error {
	if o.rom == "" {
		return usagef("--rom is required: the ROM's header says what the save holds")
	}
	if o.to == "" {
		return usagef("--to is required: %s", formatList())
	}
	to, err := parseFormat("to", o.to)
	if err != nil {
		return err
	}
	ext := strings.ToLower(filepath.Ext(input))
	if o.from == "" {
		if ext != gb.ExtSav {
			return usagef("can't tell the format of %q; pass --from (%s)", filepath.Base(input), formatList())
		}
		o.from = string(gb.MGBAApp)
	}
	from, err := parseFormat("from", o.from)
	if err != nil {
		return err
	}
	now := time.Now()
	if o.now != "" {
		n, err := strconv.ParseInt(o.now, 10, 64)
		if err != nil {
			return usagef("--now takes unix seconds, not %q", o.now)
		}
		now = time.Unix(n, 0)
	}

	header, err := readHeader(o.rom)
	if err != nil {
		return err
	}
	files, read, err := readSaveFiles(input, from)
	if err != nil {
		return err
	}
	out, err := saveporter.ConvertSave(files, from, to, header, now)
	if err != nil {
		return err
	}

	stem := strings.TrimSuffix(output, filepath.Ext(output))
	exts := make([]string, 0, len(out))
	for x := range out {
		exts = append(exts, x)
	}
	sort.Strings(exts)
	var paths []string
	for _, x := range exts {
		p := stem + x
		if !o.force && !o.dryRun {
			if _, err := os.Stat(p); err == nil {
				return fmt.Errorf("%s exists; pass --force to overwrite", p)
			}
		}
		paths = append(paths, p)
	}
	if !o.dryRun {
		for i, x := range exts {
			if err := os.WriteFile(paths[i], out[x], 0o644); err != nil {
				return err
			}
		}
	}
	cart, _ := gb.ParseCart(header)
	verb := "→"
	if o.dryRun {
		verb = "would write"
	}
	fmt.Fprintf(e.stdout, "%s %s %s\n", strings.Join(read, " + "), verb, strings.Join(paths, " + "))
	fmt.Fprintf(e.stdout, "  %s · %s · %s → %s\n", cartName(cart), cart.Mapper(), from, to)
	return nil
}

// readSaveFiles reads a save's files: the input, and the ".rtc" beside a ".srm".
func readSaveFiles(input string, from gb.Format) (gb.Files, []string, error) {
	data, err := os.ReadFile(input)
	if err != nil {
		return nil, nil, err
	}
	if from == gb.MGBAApp {
		return gb.Files{gb.ExtSav: data}, []string{input}, nil
	}
	files := gb.Files{gb.ExtSRM: data}
	read := []string{input}
	rtc := strings.TrimSuffix(input, filepath.Ext(input)) + gb.ExtRTC
	if b, err := os.ReadFile(rtc); err == nil {
		files[gb.ExtRTC] = b
		read = append(read, rtc)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	return files, read, nil
}

var romExts = []string{".gb", ".gbc", ".sgb", ".cgb"}

// readHeader reads a ROM's header, from the ROM or the first ROM in a zip.
func readHeader(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		return data, nil
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, f := range z.File {
		if !slices.Contains(romExts, strings.ToLower(filepath.Ext(f.Name))) {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		header := make([]byte, gb.HeaderSize)
		_, err = io.ReadFull(r, header)
		_ = r.Close()
		if err != nil {
			return nil, fmt.Errorf("%s in %s: %w", f.Name, path, err)
		}
		return header, nil
	}
	return nil, fmt.Errorf("%s has no Game Boy ROM in it", path)
}

func cartName(c gb.Cart) string {
	system := "GB"
	if c.CGB {
		system = "GBC"
	}
	if c.Title == "" {
		return system
	}
	if c.GameCode != "" {
		return fmt.Sprintf("%s %s (%s)", system, c.Title, c.GameCode)
	}
	return system + " " + c.Title
}
