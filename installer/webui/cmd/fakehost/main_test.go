package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

var _ hostapi.Backend = (*stagedBackend)(nil)

type countingSink struct{ markers, logs, notes int }

func (c *countingSink) Marker(string, string, string)             { c.markers++ }
func (c *countingSink) Log(string, string)                        { c.logs++ }
func (c *countingSink) Message(string, string, map[string]string) { c.notes++ }

func TestSplitFailSpec(t *testing.T) {
	id, code := splitFailSpec("50:PIP_EXTERNALLY_MANAGED")
	if id != "50" || code != "PIP_EXTERNALLY_MANAGED" {
		t.Fatalf("got %q,%q", id, code)
	}
	if id, code = splitFailSpec("60"); id != "60" || code == "" {
		t.Errorf("a spec without a code must still yield a usable fault code, got %q,%q", id, code)
	}
}

func TestTheDraftScenarioAsksForTheFingerprintOnce(t *testing.T) {
	backend := newScenario("vorlage", options{})
	_, err := backend.Connect(context.Background(), hostapi.ConnectRequest{Host: "energy-node.local", User: "pi"})
	typed, ok := err.(*hostapi.Error)
	if !ok || typed.Code != "HOSTKEY_UNKNOWN" || typed.Detail == "" {
		t.Fatalf("first connect = %v, want HOSTKEY_UNKNOWN with a fingerprint", err)
	}
	result, err := backend.Connect(context.Background(), hostapi.ConnectRequest{Host: "energy-node.local", User: "pi", AcceptFingerprint: typed.Detail})
	if err != nil || !result.Connected {
		t.Fatalf("confirmed connect = %+v, %v", result, err)
	}
}

func TestAHeldStepWaitsUntilTheRunIsCancelled(t *testing.T) {
	backend := newScenario("vorlage", options{holdStep: "40", stepDelay: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	sink := &countingSink{}
	done := make(chan error, 1)
	go func() { done <- backend.Run(ctx, hostapi.RunRequest{Mode: hostapi.ModeInstall}, sink) }()

	select {
	case err := <-done:
		t.Fatalf("the run ended before the held step was cancelled: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatalf("a cancelled held run must return the context error")
	}
	if sink.logs == 0 {
		t.Errorf("the held step printed nothing - the login URL must be in the log")
	}
}

func TestTheUpdateScenarioCarriesANewOptionalService(t *testing.T) {
	backend := newScenario("vorlage-update", options{})
	selection, _ := backend.Selection(context.Background())
	if _, listed := selection.Steps["89"]; listed || selection.Source != "node" {
		t.Errorf("selection = %+v, want a node selection that does not know step 89", selection)
	}
}

func TestTheDiagnoseShowsTheShellyWebhookOnlyWhenChosen(t *testing.T) {
	backend := newScenario("vorlage", options{})
	webhookChecks := func() []hostapi.Check {
		view, err := backend.Diagnose(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var found []hostapi.Check
		for _, c := range view.Checks {
			if strings.HasPrefix(c.Subject, "shelly-webhook-") {
				found = append(found, c)
			}
		}
		return found
	}
	if got := webhookChecks(); len(got) != 0 {
		t.Fatalf("without the opt-in the diagnose shows no webhook, got %+v", got)
	}

	steps := map[string]bool{"35": true, "83": true}
	if err := backend.SaveSelection(context.Background(), steps); err != nil {
		t.Fatal(err)
	}
	got := webhookChecks()
	if len(got) != 2 || !got[0].OK || got[0].RetryStepID != "35" || got[1].OK || got[1].Severity != "warn" {
		t.Fatalf("with the opt-in: rule ok (retry 35) and listener as hint, got %+v", got)
	}

	if err := backend.SaveSelection(context.Background(), map[string]bool{"35": true, "83": false}); err != nil {
		t.Fatal(err)
	}
	if got := webhookChecks(); len(got) != 0 {
		t.Fatalf("without the Shelly service the webhook is gone again, got %+v", got)
	}
}
