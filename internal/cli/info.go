package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/chenasraf/saveporter"
	"github.com/spf13/cobra"
)

type infoResult struct {
	File  string           `json:"file"`
	Info  *saveporter.Info `json:"info,omitempty"`
	Error string           `json:"error,omitempty"`
}

func newInfo(e env) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "info [--json] <file>...",
		Short: "Show what save state files are",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			results := make([]infoResult, 0, len(args))
			failed := false
			for _, file := range args {
				r := infoResult{File: file}
				data, err := readInput(e, file)
				if err == nil {
					var info saveporter.Info
					if info, err = saveporter.Detect(data); err == nil {
						r.Info = &info
					}
				}
				if err != nil {
					r.Error, failed = err.Error(), true
				}
				results = append(results, r)
			}
			if asJSON {
				enc := json.NewEncoder(e.stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(results); err != nil {
					return err
				}
			} else {
				printInfo(e.stdout, results)
			}
			if failed {
				return errFailed
			}
			return nil
		},
	}
	cmd.SetFlagErrorFunc(flagError)
	cmd.Flags().BoolVar(&asJSON, "json", false, "report as JSON")
	return cmd
}

func printInfo(w io.Writer, results []infoResult) {
	for i, r := range results {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, r.File)
		if r.Info == nil {
			fmt.Fprintf(w, "  %s\n", r.Error)
			continue
		}
		in := r.Info
		container := map[saveporter.Container]string{
			saveporter.ContainerRetroArch: "RetroArch",
			saveporter.ContainerMGBAPNG:   "mGBA app PNG",
			saveporter.ContainerRaw:       "none (raw core state)",
		}[in.Container]
		if in.Compression != "none" {
			container += ", " + in.Compression
		}
		game := in.Title
		if in.GameCode != "" {
			game += " (" + in.GameCode + ")"
		}
		fmt.Fprintf(w, "  system     %s (%s core)\n", strings.ToUpper(string(in.System)), coreName(in.Core))
		fmt.Fprintf(w, "  state      version %d\n", in.StateVersion)
		fmt.Fprintf(w, "  container  %s, written by %s\n", container, writersName(in.Writers))
		fmt.Fprintf(w, "  game       %s, ROM CRC %08x\n", strings.TrimSpace(game), in.ROMCRC)
	}
}
