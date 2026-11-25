package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Developer-Simon/energy-node-installer/internal/media"
)

// runMediaHelperCmd ist der interne Unterbefehl media-helper. Er steht nicht in printUsage.
func runMediaHelperCmd(args []string) int {
	files, name, rest, err := parseMediaHelperArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "media-helper:", err)
		return 125
	}
	return media.RunHelper(context.Background(), files, name, rest)
}

// runMediaShimCmd ist der interne Unterbefehl media-shim -- <name> <args...>.
func runMediaShimCmd(args []string) int {
	if len(args) < 2 || args[0] != "--" {
		fmt.Fprintln(os.Stderr, "usage: media-shim -- <name> <args...>")
		return 125
	}
	return media.RunShim(args[1], args[2:])
}

// parseMediaHelperArgs liest --log, --cancel, optional --status und danach -- <name> <args...>.
func parseMediaHelperArgs(args []string) (media.HelperFiles, string, []string, error) {
	var files media.HelperFiles
	sep := -1
	for i, a := range args {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep < 0 {
		return files, "", nil, errors.New("missing -- before the program")
	}
	rest := args[sep+1:]
	if len(rest) == 0 {
		return files, "", nil, errors.New("missing program name after --")
	}

	fs := flag.NewFlagSet(media.HelperName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&files.Log, "log", "", "")
	fs.StringVar(&files.Cancel, "cancel", "", "")
	fs.StringVar(&files.Status, "status", "", "")
	if err := fs.Parse(args[:sep]); err != nil {
		return files, "", nil, err
	}
	if fs.NArg() != 0 {
		return files, "", nil, fmt.Errorf("unexpected arguments before --: %v", fs.Args())
	}
	if files.Log == "" || files.Cancel == "" {
		return files, "", nil, errors.New("--log and --cancel are required")
	}
	return files, rest[0], rest[1:], nil
}
