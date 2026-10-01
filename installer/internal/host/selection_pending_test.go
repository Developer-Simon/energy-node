package host

import (
	"context"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/selection"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// White-box: the client is non-nil but unusable. Reading the node's
// selection.json through it would panic on the nil SSH connection, so a
// passing test also proves Selection() never went to the node while a
// selection was pending.

func TestSelectionPrefersThePendingSelectionOverTheNode(t *testing.T) {
	h := &Host{client: &transport.Client{}}
	if err := h.SaveSelection(context.Background(), map[string]bool{"83": true, "35": false}); err != nil {
		t.Fatalf("SaveSelection: %v", err)
	}

	view, err := h.Selection(context.Background())
	if err != nil {
		t.Fatalf("Selection: %v", err)
	}
	if view.Source != "pending" {
		t.Errorf("Source = %q, want pending", view.Source)
	}
	if !view.Steps["83"] || view.Steps["35"] {
		t.Errorf("Steps = %v, want the saved selection back", view.Steps)
	}
}

func TestAttachClientDropsThePendingSelection(t *testing.T) {
	h := &Host{pending: &selection.Selection{Steps: map[string]bool{"83": true}}}

	h.attachClient(&transport.Client{})

	if got := h.pendingSelection(); got != nil {
		t.Errorf("pending = %+v after a new connection, want nil: it belonged to the previous node", got)
	}
}
