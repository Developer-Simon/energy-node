package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// repoRoot walks up from the working directory until it finds a directory that
// holds both "dashboard" and "services" — the repository root. This makes the
// tool work from "go run ./cmd/schemagen" (cwd = dashboard/) as well as from
// "go test" (cwd = dashboard/cmd/schemagen/).
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if isDir(filepath.Join(dir, "dashboard")) && isDir(filepath.Join(dir, "services")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repository root (a dir with dashboard/ and services/) not found above the working directory")
		}
		dir = parent
	}
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func corePath(root string) string {
	return filepath.Join(root, "dashboard", "cmd", "schemagen", "core.schema.json")
}

func composedSchemaPath(root string) string {
	return filepath.Join(root, "dashboard", "internal", "appconfig", "config.schema.json")
}

type manifest struct {
	ServiceID string   `json:"service_id"`
	Unit      string   `json:"unit"`
	Schema    string   `json:"schema"`
	Required  []string `json:"required"`
}

// compose reads the core schema and every services/<dir>/manifest.json, splices
// each referenced fragment into properties.services.properties.<service_id>,
// lists the ids (sorted) under properties.services.required, and returns the
// pretty-printed bytes (2-space indent, trailing newline) for the committed
// file. Object keys are alphabetical — encoding/json sorts map keys.
func compose(root string) ([]byte, error) {
	coreRaw, err := os.ReadFile(corePath(root))
	if err != nil {
		return nil, err
	}
	var core map[string]any
	if err := json.Unmarshal(coreRaw, &core); err != nil {
		return nil, fmt.Errorf("%s: %w", corePath(root), err)
	}

	servicesDir := filepath.Join(root, "services")
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		return nil, err
	}

	fragments := map[string]any{}
	ids := []string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(servicesDir, entry.Name(), "manifest.json")
		raw, err := os.ReadFile(manifestPath)
		if os.IsNotExist(err) {
			continue // a service directory without a manifest.json is skipped
		}
		if err != nil {
			return nil, err
		}
		var m manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("%s: %w", manifestPath, err)
		}
		if m.ServiceID == "" || m.Schema == "" {
			return nil, fmt.Errorf("%s: service_id and schema must be non-empty", manifestPath)
		}
		if _, dup := fragments[m.ServiceID]; dup {
			return nil, fmt.Errorf("%s: duplicate service_id %q", manifestPath, m.ServiceID)
		}
		fragmentPath := filepath.Join(servicesDir, entry.Name(), m.Schema)
		fragRaw, err := os.ReadFile(fragmentPath)
		if err != nil {
			return nil, err
		}
		var fragment any
		if err := json.Unmarshal(fragRaw, &fragment); err != nil {
			return nil, fmt.Errorf("%s: %w", fragmentPath, err)
		}
		fragments[m.ServiceID] = fragment
		ids = append(ids, m.ServiceID)
	}
	sort.Strings(ids)

	properties, ok := core["properties"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("core schema has no properties object")
	}
	services, ok := properties["services"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("core schema has no properties.services object")
	}
	services["properties"] = fragments
	required := make([]any, len(ids))
	for i, id := range ids {
		required[i] = id
	}
	services["required"] = required

	out, err := json.MarshalIndent(core, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}
