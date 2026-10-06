package systemactions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// stagingProbe merkt sich, was zum Zeitpunkt des Helper-Aufrufs in der
// Staging-Datei stand.
type stagingProbe struct {
	stagedPath string
	staged     []byte
	calls      []Action
	err        error
}

func (p *stagingProbe) Execute(_ context.Context, action Action) error {
	p.calls = append(p.calls, action)
	p.staged, _ = os.ReadFile(p.stagedPath)
	return p.err
}

func TestStageAndApplyAppConfigStagesBodyCallsHelperAndCleansUp(t *testing.T) {
	dataDir := t.TempDir()
	probe := &stagingProbe{stagedPath: filepath.Join(dataDir, StagedAppConfigName)}
	body := []byte(`{"schema_version":2}` + "\n")

	if err := StageAndApplyAppConfig(context.Background(), probe, dataDir, body); err != nil {
		t.Fatal(err)
	}
	if len(probe.calls) != 1 || probe.calls[0] != ApplyAppConfig {
		t.Fatalf("calls = %v, want [%s]", probe.calls, ApplyAppConfig)
	}
	if string(probe.staged) != string(body) {
		t.Fatalf("staged = %q, want %q", probe.staged, body)
	}
	if _, err := os.Stat(probe.stagedPath); !os.IsNotExist(err) {
		t.Fatalf("Staging-Datei nicht aufgeraeumt (%v)", err)
	}
}

func TestStageAndApplyAppConfigReturnsHelperErrorAndCleansUp(t *testing.T) {
	dataDir := t.TempDir()
	want := errors.New("exit status 65")
	probe := &stagingProbe{stagedPath: filepath.Join(dataDir, StagedAppConfigName), err: want}

	err := StageAndApplyAppConfig(context.Background(), probe, dataDir, []byte(`{}`))
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if _, statErr := os.Stat(probe.stagedPath); !os.IsNotExist(statErr) {
		t.Fatalf("Staging-Datei nach Helper-Fehler nicht aufgeraeumt (%v)", statErr)
	}
}

func TestStageAndApplyAppConfigRejectsEmptyDataDir(t *testing.T) {
	probe := &stagingProbe{}
	if err := StageAndApplyAppConfig(context.Background(), probe, "", []byte(`{}`)); err == nil {
		t.Fatal("erwartet Fehler bei leerem data_dir")
	}
	if len(probe.calls) != 0 {
		t.Fatalf("Helper darf ohne Staging-Ablage nicht laufen, calls = %v", probe.calls)
	}
}
