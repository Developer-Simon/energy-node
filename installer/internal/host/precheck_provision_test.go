package host

import (
	"context"
	"errors"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// White-box (package host, not host_test): constructs a Host directly with a
// non-nil but unusable *transport.Client, so a provisioning failure proves
// Precheck returned before ever calling client.UploadFile - which would
// panic on this client's nil connection if reached.

func TestPrecheckProvisionsTheRemoteStateDirBeforeUploadingPreflight(t *testing.T) {
	orig := provisionRemoteStateDir
	t.Cleanup(func() { provisionRemoteStateDir = orig })

	var gotDir string
	provisionRemoteStateDir = func(_ context.Context, _ *transport.Client, dir string) error {
		gotDir = dir
		return errors.New("stop here - the point of this test is that we got called at all")
	}

	h := &Host{
		cfg:    Config{RemoteStateDir: "/var/lib/energy-node-installer"},
		client: &transport.Client{},
	}
	if _, err := h.Precheck(context.Background()); err == nil {
		t.Fatalf("Precheck must fail when provisioning fails")
	}
	if gotDir != "/var/lib/energy-node-installer" {
		t.Errorf("provisionRemoteStateDir dir = %q, want the configured RemoteStateDir", gotDir)
	}
}

func TestPrecheckFailsWithTheUploadFaultCodeWhenProvisioningFails(t *testing.T) {
	orig := provisionRemoteStateDir
	t.Cleanup(func() { provisionRemoteStateDir = orig })

	provisionRemoteStateDir = func(context.Context, *transport.Client, string) error {
		return errors.New("creating remote directory /var/lib/energy-node-installer: permission denied")
	}

	h := &Host{
		cfg:    Config{RemoteStateDir: "/var/lib/energy-node-installer"},
		client: &transport.Client{},
	}
	_, err := h.Precheck(context.Background())
	var apiErr *hostapi.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("Precheck error = %v (%T), want a *hostapi.Error", err, err)
	}
	if apiErr.Code != "PREFLIGHT_UPLOAD_FAILED" {
		t.Errorf("code = %q, want PREFLIGHT_UPLOAD_FAILED - the same fault the operator sees for a plain upload failure", apiErr.Code)
	}
}
