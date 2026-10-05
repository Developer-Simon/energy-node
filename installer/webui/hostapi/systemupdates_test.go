package hostapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

func TestSystemUpdatesEndpointCountsOnTheLastListsAndRefreshesOnlyOnPost(t *testing.T) {
	server, fake := newTestServer(t, nil)
	fake.SystemUpdatesView = &hostapi.SystemUpdates{Count: 2, CheckedAt: "2026-10-04T06:12:00+00:00",
		Packages: []hostapi.SystemPackage{{Name: "libssl3", From: "3.0.11", To: "3.0.13"}, {Name: "openssl", From: "3.0.11", To: "3.0.13"}}}
	connectFirst(t, server)

	rec := do(t, server, http.MethodGet, "/api/system-updates", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var got hostapi.SystemUpdates
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Count != 2 || got.Packages[1].Name != "openssl" {
		t.Fatalf("body = %s (%v)", rec.Body.String(), err)
	}
	if want := []bool{false}; !equalBools(fake.SystemUpdatesCalls, want) {
		t.Fatalf("calls = %v, want %v", fake.SystemUpdatesCalls, want)
	}

	if rec := do(t, server, http.MethodPost, "/api/system-updates/refresh", ""); rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d: %s", rec.Code, rec.Body.String())
	}
	if want := []bool{false, true}; !equalBools(fake.SystemUpdatesCalls, want) {
		t.Fatalf("calls = %v, want %v", fake.SystemUpdatesCalls, want)
	}

	if rec := do(t, server, http.MethodPost, "/api/system-updates", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST on the cached check: status = %d, want 405", rec.Code)
	}
	if rec := do(t, server, http.MethodGet, "/api/system-updates/refresh", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET on refresh: status = %d, want 405", rec.Code)
	}
}

func TestSystemUpdatesEndpointSendsNullWhenTheCountIsUnknown(t *testing.T) {
	server, _ := newTestServer(t, nil)
	connectFirst(t, server)
	rec := do(t, server, http.MethodGet, "/api/system-updates", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "null" {
		t.Fatalf("status = %d body = %q, want 200 null", rec.Code, rec.Body.String())
	}
}

func TestSystemUpdatesEndpointPassesAFailedRefreshOn(t *testing.T) {
	server, fake := newTestServer(t, nil)
	fake.SystemUpdatesErr = &hostapi.Error{Code: "APT_UPDATE_FAILED", Detail: "E: Failed to fetch", Status: http.StatusBadGateway}
	connectFirst(t, server)
	rec := do(t, server, http.MethodPost, "/api/system-updates/refresh", "")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "APT_UPDATE_FAILED") {
		t.Fatalf("status = %d body = %s, want 502 APT_UPDATE_FAILED", rec.Code, rec.Body.String())
	}
}

func TestSystemUpdatesEndpointIsRefusedWithoutAConnection(t *testing.T) {
	server, _ := newTestServer(t, nil)
	if rec := do(t, server, http.MethodGet, "/api/system-updates", ""); rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 NOT_CONNECTED", rec.Code)
	}
}

func equalBools(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
