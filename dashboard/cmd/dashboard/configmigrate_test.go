package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/systemactions"
)

// fakeHelperRunner steht fuer den root-eigenen apply-app-config-Verb: es
// prueft die Aufrufform, liest die vom Dashboard gestagete Datei und
// "installiert" sie an einem vom Test kontrollierten Ort.
type fakeHelperRunner struct {
	stagedPath string
	targetPath string
	lastAction string
	failWith   error
}

func (r *fakeHelperRunner) Run(_ context.Context, name string, args ...string) error {
	if name != "sudo" || len(args) == 0 {
		return errors.New("unexpected invocation")
	}
	r.lastAction = args[len(args)-1]
	if r.failWith != nil {
		return r.failWith
	}
	data, err := os.ReadFile(r.stagedPath)
	if err != nil {
		return err
	}
	return os.WriteFile(r.targetPath, data, 0o664)
}

func TestPersistV1MigrationStagesAndCallsTheHelper(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "config.json")
	runner := &fakeHelperRunner{
		stagedPath: filepath.Join(dataDir, stagedAppConfigName),
		targetPath: target,
	}
	executor := systemactions.NewExecutor(runner, "/usr/local/sbin/energy-node-dashboard-system-action")

	migrated := []byte(`{"schema_version":2}` + "\n")
	if err := persistV1Migration(context.Background(), executor, dataDir, migrated); err != nil {
		t.Fatalf("persistV1Migration: %v", err)
	}

	if runner.lastAction != string(systemactions.ApplyAppConfig) {
		t.Fatalf("helper action = %q, want %q", runner.lastAction, systemactions.ApplyAppConfig)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("target nicht geschrieben: %v", err)
	}
	if string(got) != string(migrated) {
		t.Fatalf("target = %q, want %q", got, migrated)
	}
	if _, err := os.Stat(runner.stagedPath); !os.IsNotExist(err) {
		t.Fatalf("Staging-Datei nicht aufgeraeumt (%v)", err)
	}
}

func TestPersistV1MigrationReturnsHelperErrorAndClearsStaging(t *testing.T) {
	dataDir := t.TempDir()
	runner := &fakeHelperRunner{
		stagedPath: filepath.Join(dataDir, stagedAppConfigName),
		failWith:   errors.New("exit status 65"),
	}
	executor := systemactions.NewExecutor(runner, "/usr/local/sbin/energy-node-dashboard-system-action")

	err := persistV1Migration(context.Background(), executor, dataDir, []byte(`{"schema_version":2}`))
	if err == nil {
		t.Fatal("erwartet Fehler vom Helper")
	}
	if _, statErr := os.Stat(runner.stagedPath); !os.IsNotExist(statErr) {
		t.Fatalf("Staging-Datei nach Helper-Fehler nicht aufgeraeumt (%v)", statErr)
	}
}
