package main

import (
	"fmt"
	"io"
	"os"
)

var version = "dev"

const versionCommand = "version"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 1 && arguments[0] == versionCommand {
		_, err := fmt.Fprintf(stdout, "music-ops %s\n", version)
		if err != nil {
			return 1
		}
		return 0
	}
	_, _ = fmt.Fprintln(stderr, "usage: music-ops version")
	return 1
}
