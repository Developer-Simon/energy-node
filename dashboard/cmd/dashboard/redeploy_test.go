// dashboard/cmd/dashboard/redeploy_test.go
package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildRedeployHandlerServesTheBootstrapEndpointWithoutAToken(t *testing.T) {
	root := t.TempDir()
	candidate := filepath.Join(root, "candidate")
	os.MkdirAll(candidate, 0o755)
	os.WriteFile(filepath.Join(candidate, "manifest.json"), []byte(`{"version":"1.5.0","steps":[]}`), 0o644)
	os.WriteFile(filepath.Join(root, "installed-manifest.json"), []byte(`{"version":"1.4.0"}`), 0o644)
	os.WriteFile(filepath.Join(root, "selection.json"), []byte(`{"steps":{}}`), 0o644)
	jobDir := filepath.Join(root, "job")
	os.MkdirAll(jobDir, 0o755)

	handler, err := buildRedeployHandler(redeployConfig{
		candidateBundleDir: candidate, installedManifestPath: filepath.Join(root, "installed-manifest.json"),
		selectionPath: filepath.Join(root, "selection.json"), jobDir: jobDir,
	})
	if err != nil {
		t.Fatalf("buildRedeployHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/redeploy/api/bootstrap", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
