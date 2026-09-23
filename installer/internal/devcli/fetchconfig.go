package devcli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// FetchConfigArgs configures one call to RunFetchConfig.
type FetchConfigArgs struct {
	Client            *transport.Client
	RemoteConfigPath  string                            // e.g. /etc/energy-node/config.json
	LocalTemplatePath string                            // e.g. services/energy-node.config.json
	Confirm           func(prompt string) (bool, error) // nil uses a real terminal prompt
	Stdout            io.Writer

	// Devices also pulls the operator's device files -- every *.json a
	// service ships as a template that the dashboard then edits on the node
	// (*_devices.json, automation_rules.json) -- from RemoteDevicesDir back
	// into RepoRoot/services/<dir>/.
	Devices          bool
	RepoRoot         string
	RemoteDevicesDir string // e.g. /home/<user>/devices
}

// RunFetchConfig downloads the node's config.json into the repository
// template, asking first if that template already exists -- see this task's
// rationale for why declining must leave the existing file untouched.
func RunFetchConfig(ctx context.Context, args FetchConfigArgs) error {
	tmp, err := os.CreateTemp("", "energy-node-config-*.json")
	if err != nil {
		return fmt.Errorf("creating a temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	if err := args.Client.DownloadFile(args.RemoteConfigPath, tmpPath); err != nil {
		return fmt.Errorf("downloading %s: %w", args.RemoteConfigPath, err)
	}

	if _, err := os.Stat(args.LocalTemplatePath); err == nil {
		confirm := args.Confirm
		if confirm == nil {
			confirm = defaultConfirm
		}
		ok, err := confirm(fmt.Sprintf("%s already exists. Overwrite it with the node's current config.json?", args.LocalTemplatePath))
		if err != nil {
			return fmt.Errorf("confirming overwrite: %w", err)
		}
		if !ok {
			return fmt.Errorf("aborted: %s was not overwritten", args.LocalTemplatePath)
		}
	}

	content, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("reading downloaded config.json: %w", err)
	}
	if err := os.WriteFile(args.LocalTemplatePath, content, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", args.LocalTemplatePath, err)
	}

	fmt.Fprintf(args.Stdout, "Wrote %s from %s.\n", args.LocalTemplatePath, args.RemoteConfigPath)

	if args.Devices {
		if err := fetchDeviceFiles(args); err != nil {
			return err
		}
	}
	fmt.Fprintln(args.Stdout, "Check before committing: ./scripts/dev/check_tracked_secrets.sh")
	return nil
}

// operatorDeviceFiles lists the device files in the checkout that the node
// keeps as operator data, mapped to their local path. It is the complement
// of service_step_is_artefact in scripts/bootstrap/lib/service_step.sh:
// schemas and presets are shipped on every deploy and never differ on the
// node, and manifest.json/config.schema.json never go there at all.
func operatorDeviceFiles(repoRoot string) (map[string]string, error) {
	manifests, err := filepath.Glob(filepath.Join(repoRoot, "services", "*", "manifest.json"))
	if err != nil {
		return nil, err
	}
	files := map[string]string{}
	for _, manifest := range manifests {
		jsons, err := filepath.Glob(filepath.Join(filepath.Dir(manifest), "*.json"))
		if err != nil {
			return nil, err
		}
		for _, local := range jsons {
			name := filepath.Base(local)
			switch {
			case name == "manifest.json", name == "config.schema.json",
				strings.HasSuffix(name, ".schema.json"), strings.HasSuffix(name, "_presets.json"):
				continue
			}
			files[name] = local
		}
	}
	return files, nil
}

// fetchDeviceFiles downloads every operator device file the node has, then
// asks once before overwriting the checkout's copies. A file the node does
// not have (a service that was never installed) is skipped, not an error.
func fetchDeviceFiles(args FetchConfigArgs) error {
	files, err := operatorDeviceFiles(args.RepoRoot)
	if err != nil {
		return fmt.Errorf("listing device files in %s: %w", args.RepoRoot, err)
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	tmpDir, err := os.MkdirTemp("", "energy-node-devices-*")
	if err != nil {
		return fmt.Errorf("creating a temporary directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	var fetched []string
	for _, name := range names {
		remote := path.Join(args.RemoteDevicesDir, name)
		if err := args.Client.DownloadFile(remote, filepath.Join(tmpDir, name)); err != nil {
			fmt.Fprintf(args.Stdout, "Skipped %s: %v\n", remote, err)
			continue
		}
		fetched = append(fetched, name)
	}
	if len(fetched) == 0 {
		return nil
	}

	confirm := args.Confirm
	if confirm == nil {
		confirm = defaultConfirm
	}
	ok, err := confirm(fmt.Sprintf("Overwrite %d device file(s) under services/ with the node's copies (%s)?", len(fetched), strings.Join(fetched, ", ")))
	if err != nil {
		return fmt.Errorf("confirming overwrite: %w", err)
	}
	if !ok {
		return fmt.Errorf("aborted: device files were not overwritten")
	}
	for _, name := range fetched {
		content, err := os.ReadFile(filepath.Join(tmpDir, name))
		if err != nil {
			return fmt.Errorf("reading downloaded %s: %w", name, err)
		}
		if err := os.WriteFile(files[name], content, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", files[name], err)
		}
		fmt.Fprintf(args.Stdout, "Wrote %s.\n", files[name])
	}
	return nil
}
