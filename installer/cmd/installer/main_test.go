package main

import (
	"path/filepath"
	"testing"
)

func TestParseDeployFlagsAppliesDefaults(t *testing.T) {
	cfg, err := parseDeployFlags(nil, "/repo")
	if err != nil {
		t.Fatalf("parseDeployFlags: %v", err)
	}
	if cfg.common.repoRoot != "/repo" {
		t.Errorf("expected the default repo root to be used, got %q", cfg.common.repoRoot)
	}
	if cfg.common.arch != "armv6" {
		t.Errorf("expected the default arch to be armv6, got %q", cfg.common.arch)
	}
	if cfg.only != "" || cfg.dryRun || cfg.forceConfig {
		t.Errorf("expected zero-value flags by default, got %+v", cfg)
	}
}

func TestParseDeployFlagsAppliesOverrides(t *testing.T) {
	cfg, err := parseDeployFlags([]string{
		"--only", "dashboard", "--dry-run", "--force-config",
		"--arch", "arm64", "--host", "10.0.0.5", "--repo", "/other",
	}, "/repo")
	if err != nil {
		t.Fatalf("parseDeployFlags: %v", err)
	}
	if cfg.only != "dashboard" || !cfg.dryRun || !cfg.forceConfig {
		t.Errorf("expected the given flags to apply, got %+v", cfg)
	}
	if cfg.common.arch != "arm64" || cfg.common.host != "10.0.0.5" || cfg.common.repoRoot != "/other" {
		t.Errorf("expected the common flag overrides to apply, got %+v", cfg.common)
	}
}

func TestParseDeployFlagsDevUnsignedDefaultsToFalse(t *testing.T) {
	cfg, err := parseDeployFlags(nil, "/repo")
	if err != nil {
		t.Fatalf("parseDeployFlags: %v", err)
	}
	if cfg.common.devUnsigned {
		t.Errorf("expected --dev-unsigned to default to false")
	}
}

func TestParseDeployFlagsAppliesDevUnsigned(t *testing.T) {
	cfg, err := parseDeployFlags([]string{"--dev-unsigned"}, "/repo")
	if err != nil {
		t.Fatalf("parseDeployFlags: %v", err)
	}
	if !cfg.common.devUnsigned {
		t.Errorf("expected --dev-unsigned to apply")
	}
}

func TestParseEnsureSecretsFlagsAppliesDevUnsigned(t *testing.T) {
	cfg, err := parseEnsureSecretsFlags([]string{"--dev-unsigned"}, "/repo")
	if err != nil {
		t.Fatalf("parseEnsureSecretsFlags: %v", err)
	}
	if !cfg.common.devUnsigned {
		t.Errorf("expected --dev-unsigned to apply")
	}
}

func TestParseFetchConfigFlagsDefaultsTheLocalTemplatePath(t *testing.T) {
	cfg, err := parseFetchConfigFlags(nil, "/repo")
	if err != nil {
		t.Fatalf("parseFetchConfigFlags: %v", err)
	}
	want := filepath.Join("/repo", "services", "energy-node.config.json")
	if cfg.localTemplatePath != want {
		t.Errorf("got %q, want %q", cfg.localTemplatePath, want)
	}
}

func TestParseFetchConfigFlagsAppliesDevices(t *testing.T) {
	cfg, err := parseFetchConfigFlags([]string{"--devices"}, "/repo")
	if err != nil {
		t.Fatalf("parseFetchConfigFlags: %v", err)
	}
	if !cfg.devices {
		t.Errorf("expected --devices to apply")
	}
}

func TestParseRestartFlagsAppliesOnly(t *testing.T) {
	cfg, err := parseRestartFlags([]string{"--only", "shelly"}, "/repo")
	if err != nil {
		t.Fatalf("parseRestartFlags: %v", err)
	}
	if cfg.only != "shelly" {
		t.Errorf("got only %q, want shelly", cfg.only)
	}
}

func TestParseEnsureSecretsFlagsDefaultsTheSecretPaths(t *testing.T) {
	cfg, err := parseEnsureSecretsFlags(nil, "/repo")
	if err != nil {
		t.Fatalf("parseEnsureSecretsFlags: %v", err)
	}
	if cfg.mqttSecretPath != filepath.Join("/repo", "secrets", "mqtt.pw") {
		t.Errorf("unexpected mqtt secret path: %q", cfg.mqttSecretPath)
	}
	if cfg.adminSecretPath != filepath.Join("/repo", "secrets", "dashboard-admin.pw") {
		t.Errorf("unexpected admin secret path: %q", cfg.adminSecretPath)
	}
}

func TestDefaultPasswordFilePathJoinsSecretsDirectory(t *testing.T) {
	got := defaultPasswordFilePath("/repo")
	want := filepath.Join("/repo", "secrets", "system-ssh.pw")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
