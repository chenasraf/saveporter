// Command saveporter converts emulator save states between emulators.
package main

import (
	"os"

	"github.com/chenasraf/saveporter/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
