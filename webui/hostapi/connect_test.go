package hostapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

func TestConnectPassesTheRequestToTheBackend(t *testing.T) {
	server, fake := newTestServer(t, nil)
	rec := do(t, server, http.MethodPost, "/api/connect",
		`{"host":"node.local","user":"orgelbau","kind":"password","secret":"hunter2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if fake.LastConnect.Host != "node.local" || fake.LastConnect.Secret != "hunter2" {
		t.Errorf("the backend got %+v", fake.LastConnect)
	}
}

func TestConnectNeverEchoesTheSecret(t *testing.T) {
	server, _ := newTestServer(t, nil)
	rec := do(t, server, http.MethodPost, "/api/connect",
		`{"host":"node.local","user":"orgelbau","kind":"password","secret":"hunter2"}`)
	if body := rec.Body.String(); containsSecret(body, "hunter2") {
		t.Fatalf("the response carries the password back: %s", body)
	}
}

func containsSecret(body, secret string) bool {
	return len(secret) > 0 && len(body) > 0 && jsonContains(body, secret)
}

func jsonContains(body, needle string) bool {
	return len(body) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(body); i++ {
			if body[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

func TestAnUnknownHostKeyBecomesA409WithTheFingerprint(t *testing.T) {
	server, fake := newTestServer(t, nil)
	fake.ConnectErr = &hostapi.Error{
		Code:   "HOSTKEY_UNKNOWN",
		Detail: "SHA256:abcdef0123456789",
		Status: http.StatusConflict,
	}
	rec := do(t, server, http.MethodPost, "/api/connect", `{"host":"node.local","user":"pi","kind":"password","secret":"x"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if payload["error"] != "HOSTKEY_UNKNOWN" || payload["detail"] != "SHA256:abcdef0123456789" {
		t.Errorf("payload = %v, want the fingerprint in detail", payload)
	}
}

func TestAFailedLoginIs401(t *testing.T) {
	server, fake := newTestServer(t, nil)
	fake.ConnectErr = &hostapi.Error{Code: "AUTH_FAILED", Status: http.StatusUnauthorized}
	rec := do(t, server, http.MethodPost, "/api/connect", `{"host":"h","user":"u","kind":"password","secret":"x"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAMalformedBodyIs400(t *testing.T) {
	server, _ := newTestServer(t, nil)
	rec := do(t, server, http.MethodPost, "/api/connect", `{"host":`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestConnectRequiresHostAndUser(t *testing.T) {
	server, _ := newTestServer(t, nil)
	rec := do(t, server, http.MethodPost, "/api/connect", `{"kind":"password","secret":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a request without a host", rec.Code)
	}
}

func TestKeypairIsRefusedBeforeAConnection(t *testing.T) {
	server, _ := newTestServer(t, nil)
	rec := do(t, server, http.MethodPost, "/api/keypair", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 NOT_CONNECTED", rec.Code)
	}
}

func TestKeypairAfterAConnectionReturnsThePublicKey(t *testing.T) {
	server, _ := newTestServer(t, nil)
	if rec := do(t, server, http.MethodPost, "/api/connect", `{"host":"h","user":"u","kind":"password","secret":"pw"}`); rec.Code != http.StatusOK {
		t.Fatalf("connect failed: %s", rec.Body.String())
	}
	rec := do(t, server, http.MethodPost, "/api/keypair", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got hostapi.KeypairResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if got.PublicKey == "" || !got.Installed {
		t.Errorf("KeypairResult = %+v, want an installed public key", got)
	}
}

func TestADashboardHostNeedsNoConnection(t *testing.T) {
	server, fake := newTestServer(t, nil)
	fake.Description.Host = hostapi.HostDashboard
	fake.Description.NeedsConnection = false
	rec := do(t, server, http.MethodGet, "/api/precheck", "")
	if rec.Code == http.StatusConflict {
		t.Fatalf("a host without a connection screen must not be blocked by NOT_CONNECTED")
	}
}
