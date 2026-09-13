// Package devcli wires Plan B-I's transport, bundle, selection and steps
// packages into the four developer-CLI subcommands from E12: deploy,
// ensure-secrets, fetch-config and diagnose. cmd/installer stays a thin flag
// parser around this package so its orchestration logic is testable without
// spawning the compiled binary.
package devcli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Target names the node a subcommand acts on -- literally TARGET_HOST,
// TARGET_USER and TARGET_BASE from scripts/deploy/deploy_lib.sh's
// parse_deploy_args, read the same way from the same file.
type Target struct {
	Host string
	User string
	Base string
}

// DefaultDeployTargetPath is where every scripts/deploy/*.sh script already
// looks for its target, so the CLI defaults to the same file rather than
// asking developers to configure their target twice.
func DefaultDeployTargetPath(repoRoot string) string {
	return filepath.Join(repoRoot, "secrets", "deploy-target.env")
}

// LoadTarget reads a deploy-target.env-formatted file: "KEY=VALUE" lines,
// blank lines and "#"-comments ignored. This is deliberately not a shell
// parser -- the file has never held more than a handful of plain
// assignments, and never needed quoting or variable expansion.
func LoadTarget(path string) (Target, error) {
	f, err := os.Open(path)
	if err != nil {
		return Target{}, fmt.Errorf("opening target file %s: %w", path, err)
	}
	defer f.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return Target{}, fmt.Errorf("reading target file %s: %w", path, err)
	}

	target := Target{
		Host: values["TARGET_HOST"],
		User: values["TARGET_USER"],
		Base: values["TARGET_BASE"],
	}
	if target.Base == "" && target.User != "" {
		target.Base = "/home/" + target.User
	}
	return target, nil
}

// Override replaces each field with the given value if it is non-empty,
// matching how --host/--user/--base take precedence over deploy-target.env
// in every scripts/deploy/*.sh script today.
func (t Target) Override(host, user, base string) Target {
	if host != "" {
		t.Host = host
	}
	if user != "" {
		t.User = user
	}
	if base != "" {
		t.Base = base
	}
	return t
}
