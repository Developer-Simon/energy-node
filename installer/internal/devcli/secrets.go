package devcli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadOrPromptSecret reads a cached secret from path if it exists, trimming
// exactly one trailing newline the way ensure_remote_secrets.sh's own local
// cache files are written today. If the file is missing, it calls prompt
// with label and caches the result at path with mode 0600 before returning
// it -- the same "ask once, reuse afterwards" behaviour as the bash scripts
// it replaces (ensure_remote_secrets.sh, ensure_remote_dashboard_auth.sh).
func LoadOrPromptSecret(path string, label string, prompt func(label string) (string, error)) (string, error) {
	if existing, err := os.ReadFile(path); err == nil {
		return strings.TrimRight(string(existing), "\n"), nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("reading cached secret %s: %w", path, err)
	}

	value, err := prompt(label)
	if err != nil {
		return "", fmt.Errorf("prompting for %s: %w", label, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("creating directory for %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		return "", fmt.Errorf("caching secret at %s: %w", path, err)
	}
	return value, nil
}
