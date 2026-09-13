package devcli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/devcli"
)

func TestLoadOrPromptSecretUsesAnExistingFileWithoutPrompting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mqtt.pw")
	if err := os.WriteFile(path, []byte("geheim123\n"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	prompted := false
	got, err := devcli.LoadOrPromptSecret(path, "MQTT password", func(string) (string, error) {
		prompted = true
		return "should not be used", nil
	})
	if err != nil {
		t.Fatalf("LoadOrPromptSecret: %v", err)
	}
	if got != "geheim123" {
		t.Fatalf("expected the trailing newline to be trimmed, got %q", got)
	}
	if prompted {
		t.Fatalf("must not prompt when a cached secret already exists")
	}
}

func TestLoadOrPromptSecretPromptsAndCachesAMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "mqtt.pw")

	got, err := devcli.LoadOrPromptSecret(path, "MQTT password", func(label string) (string, error) {
		if label != "MQTT password" {
			t.Errorf("unexpected label passed to prompt: %q", label)
		}
		return "frisch-gesetzt", nil
	})
	if err != nil {
		t.Fatalf("LoadOrPromptSecret: %v", err)
	}
	if got != "frisch-gesetzt" {
		t.Fatalf("unexpected secret: %q", got)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected the secret to be cached at %s: %v", path, err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected mode 0600, got %v", info.Mode().Perm())
	}
	cached, err := os.ReadFile(path)
	if err != nil || string(cached) != "frisch-gesetzt" {
		t.Fatalf("unexpected cached content: %q, err=%v", cached, err)
	}
}

func TestLoadOrPromptSecretPropagatesAPromptError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mqtt.pw")
	_, err := devcli.LoadOrPromptSecret(path, "MQTT password", func(string) (string, error) {
		return "", fmt.Errorf("boom")
	})
	if err == nil {
		t.Fatalf("expected the prompt's error to propagate")
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Fatalf("must not cache anything when the prompt fails")
	}
}
