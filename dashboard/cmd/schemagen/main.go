// Command schemagen composes dashboard/internal/appconfig/config.schema.json
// from the hand-maintained core schema (core.schema.json, next to this file)
// and one schema fragment per device service (services/<dir>/config.schema.json,
// discovered through services/<dir>/manifest.json).
//
// The composed file is committed and embedded by internal/appconfig; Go reads
// only the composed file at runtime — there is no runtime merge. Run
//
//	(cd dashboard && go run ./cmd/schemagen)
//
// after adding or changing a service manifest or fragment. "-check" exits 1 on
// drift and is wired into git-hooks/pre-commit and TestComposedSchemaIsCommitted.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
)

func main() {
	check := flag.Bool("check", false, "exit 1 if the committed schema differs from the freshly composed one")
	flag.Parse()

	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "schemagen:", err)
		os.Exit(1)
	}
	composed, err := compose(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "schemagen:", err)
		os.Exit(1)
	}
	target := composedSchemaPath(root)

	if *check {
		current, err := os.ReadFile(target)
		if err != nil {
			fmt.Fprintln(os.Stderr, "schemagen:", err)
			os.Exit(1)
		}
		if !bytes.Equal(current, composed) {
			fmt.Fprintf(os.Stderr, "schemagen: %s is stale — run: (cd dashboard && go run ./cmd/schemagen)\n", target)
			os.Exit(1)
		}
		return
	}

	if err := os.WriteFile(target, composed, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "schemagen:", err)
		os.Exit(1)
	}
	fmt.Println("wrote", target)
}
