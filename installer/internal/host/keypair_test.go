package host_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/host"
)

func TestGenerateEd25519WritesAKeyPairWithTightPermissions(t *testing.T) {
	dir := t.TempDir()
	privatePath, publicLine, err := host.GenerateEd25519(dir)
	if err != nil {
		t.Fatalf("GenerateEd25519: %v", err)
	}
	if !strings.HasPrefix(publicLine, "ssh-ed25519 ") {
		t.Errorf("public line = %q, want an OpenSSH ed25519 line", publicLine)
	}
	if !strings.HasSuffix(publicLine, " energy-node-installer") {
		t.Errorf("public line = %q, want the installer's comment", publicLine)
	}
	info, err := os.Stat(privatePath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600 - ssh refuses a world-readable private key", info.Mode().Perm())
	}
	if filepath.Dir(privatePath) != dir {
		t.Errorf("the key landed in %q, want %q", filepath.Dir(privatePath), dir)
	}
	data, err := os.ReadFile(privatePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasPrefix(string(data), "-----BEGIN OPENSSH PRIVATE KEY-----") {
		t.Errorf("the private key is not in OpenSSH format:\n%s", data)
	}
}

func TestGenerateEd25519DoesNotOverwriteAnExistingKey(t *testing.T) {
	dir := t.TempDir()
	first, _, err := host.GenerateEd25519(dir)
	if err != nil {
		t.Fatalf("GenerateEd25519: %v", err)
	}
	before, _ := os.ReadFile(first)

	second, _, err := host.GenerateEd25519(dir)
	if err != nil {
		t.Fatalf("second GenerateEd25519: %v", err)
	}
	after, _ := os.ReadFile(second)
	if string(before) != string(after) {
		t.Errorf("the second call replaced an existing private key")
	}
}
