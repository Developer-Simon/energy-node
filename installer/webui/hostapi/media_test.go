package hostapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
	"github.com/Developer-Simon/energy-node-webui/hostapi/hostapitest"
)

func TestMediaStatusComesFromTheProvider(t *testing.T) {
	server, _ := newTestServer(t, nil)
	rec := do(t, server, http.MethodGet, "/api/media/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var view struct {
		Imager struct {
			Kind string `json:"kind"`
		} `json:"imager"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.Imager.Kind != "flatpak" {
		t.Errorf("imager.kind = %q, want flatpak", view.Imager.Kind)
	}
}

func TestMediaDrivesHidesInternalByDefault(t *testing.T) {
	server, _ := newTestServer(t, nil)
	var plain []hostapi.MediaDrive
	if err := json.Unmarshal(do(t, server, http.MethodGet, "/api/media/drives", "").Body.Bytes(), &plain); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(plain) != 1 || plain[0].Path != "/dev/sdb" {
		t.Errorf("without internal: %+v, want only /dev/sdb", plain)
	}
	var all []hostapi.MediaDrive
	if err := json.Unmarshal(do(t, server, http.MethodGet, "/api/media/drives?internal=1", "").Body.Bytes(), &all); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("with internal=1: %d drives, want 2", len(all))
	}
}

func TestMediaWritePassesTheRequest(t *testing.T) {
	server, fake := newTestServer(t, nil)
	body := `{"device":"pi4","drive":"/dev/sdb","confirm":"yes","settings":{"hostname":"energy-node","user":"node","password":"x","timezone":"Europe/Berlin","keyboard":"de","wifi":null}}`
	rec := do(t, server, http.MethodPost, "/api/media/write", body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	if fake.LastMediaWrite.Settings.Hostname != "energy-node" || fake.LastMediaWrite.Device != "pi4" {
		t.Errorf("the provider got %+v", fake.LastMediaWrite)
	}
}

func TestMediaWriteMapsBackendErrors(t *testing.T) {
	server, fake := newTestServer(t, nil)
	fake.MediaWriteErr = &hostapi.Error{Code: "MEDIA_DRIVE_GONE", Status: http.StatusConflict}
	rec := do(t, server, http.MethodPost, "/api/media/write", `{"device":"pi4","drive":"/dev/sdb"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"error":"MEDIA_DRIVE_GONE"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestMediaJobFollowsTheScriptedSequence(t *testing.T) {
	server, _ := newTestServer(t, nil)
	if rec := do(t, server, http.MethodPost, "/api/media/write", `{"device":"pi4","drive":"/dev/sdb"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("write status = %d", rec.Code)
	}
	var states []string
	for i := 0; i < 4; i++ {
		var job hostapi.MediaJob
		if err := json.Unmarshal(do(t, server, http.MethodGet, "/api/media/job", "").Body.Bytes(), &job); err != nil {
			t.Fatalf("decode: %v", err)
		}
		states = append(states, job.State)
		if i == 3 && (job.State != "done" || job.Target == nil || job.Target.Host != "energy-node.local") {
			t.Errorf("last job = %+v, want done with the target", job)
		}
	}
	if states[0] != "running" || states[3] != "done" {
		t.Errorf("states = %v", states)
	}
}

func TestMediaRoutesNeedTheProvider(t *testing.T) {
	fake := hostapitest.NewFake()
	server, err := hostapi.New(hostapi.Options{
		Backend:  plainBackend{Backend: fake},
		Catalogs: testCatalogs(t),
		Token:    testToken,
		Language: "de",
	})
	if err != nil {
		t.Fatalf("hostapi.New: %v", err)
	}
	rec := do(t, server, http.MethodGet, "/api/media/status", "")
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"error":"NOT_SUPPORTED"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestMediaRejectsWrongMethods(t *testing.T) {
	server, _ := newTestServer(t, nil)
	for _, c := range []struct{ method, path string }{
		{http.MethodPut, "/api/media/write"},
		{http.MethodPost, "/api/media/job"},
		{http.MethodGet, "/api/media/cancel"},
	} {
		rec := do(t, server, c.method, c.path, "")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: status = %d, want 405", c.method, c.path, rec.Code)
		}
	}
}

// plainBackend bettet nur das Backend ein, ohne MediaProvider.
type plainBackend struct {
	hostapi.Backend
}

func TestMediaCatalogCarriesTheDetectedWifiName(t *testing.T) {
	server, _ := newTestServer(t, nil)
	rec := do(t, server, http.MethodGet, "/api/media/catalog", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var catalog struct {
		Defaults struct {
			WifiSSID string `json:"wifi_ssid"`
		} `json:"defaults"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if catalog.Defaults.WifiSSID != "Werkstatt" {
		t.Errorf("defaults.wifi_ssid = %q, want Werkstatt", catalog.Defaults.WifiSSID)
	}
}

func TestMediaWifiCheck(t *testing.T) {
	server, _ := newTestServer(t, nil)
	cases := map[string]string{
		`{"ssid":"Werkstatt","password":"werkstatt-wlan"}`: "match",
		`{"ssid":"Werkstatt","password":"falsch-falsch"}`:  "mismatch",
		`{"ssid":"Nachbar","password":"werkstatt-wlan"}`:   "unknown",
	}
	for body, want := range cases {
		rec := do(t, server, http.MethodPost, "/api/media/wifi-check", body)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d: %s", body, rec.Code, rec.Body.String())
		}
		var view hostapi.MediaWifiCheckResult
		if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if view.Result != want {
			t.Errorf("%s: result = %q, want %q", body, view.Result, want)
		}
	}
	if rec := do(t, server, http.MethodGet, "/api/media/wifi-check", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", rec.Code)
	}
}
