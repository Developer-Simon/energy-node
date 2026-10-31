package hostapi

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestRunFinishPayloadCarriesTheDetailOfEveryFailure(t *testing.T) {
	redactor := NewRedactor("hunter2-secret")
	cases := map[string]struct {
		err        error
		wantCode   string
		wantDetail string
	}{
		"untyped": {errors.New("no bootstrap script found for step 10"), "BACKEND_ERROR", "no bootstrap script found for step 10"},
		"typed":   {&Error{Code: "PACKAGE_STAGE_FAILED", Detail: "scp: broken pipe"}, "PACKAGE_STAGE_FAILED", "scp: broken pipe"},
		"secret":  {errors.New("login with hunter2-secret failed"), "BACKEND_ERROR", "login with " + redactor.Line("hunter2-secret") + " failed"},
	}
	for name, tc := range cases {
		payload := runFinishPayload(NewBus(10), "run-1", tc.err, redactor)
		if payload["code"] != tc.wantCode || payload["detail"] != tc.wantDetail {
			t.Errorf("%s: payload = %v, want code %s detail %q", name, payload, tc.wantCode, tc.wantDetail)
		}
	}
}

func TestRunRequestSecretsCoverEveryUserPassword(t *testing.T) {
	req := RunRequest{
		MQTTPassword:  "mqtt-geheim",
		AdminPassword: "admin-geheim",
		DashboardPrefs: json.RawMessage(`{"theme":"stromblau","users":[` +
			`{"name":"anna","template":"operate","password":"anna-geheim-1"},` +
			`{"name":"ben","template":"view","password":"ben-geheim-22"}]}`),
	}
	got := req.Secrets()
	for _, want := range []string{"mqtt-geheim", "admin-geheim", "anna-geheim-1", "ben-geheim-22"} {
		found := false
		for _, s := range got {
			found = found || s == want
		}
		if !found {
			t.Errorf("Secrets() = %q, misses %q", got, want)
		}
	}
}

func TestRunRequestSecretsTolerateBadDashboardPrefs(t *testing.T) {
	cases := map[string]json.RawMessage{
		"missing":          nil,
		"null":             json.RawMessage(`null`),
		"invalid json":     json.RawMessage(`{"users":[`),
		"users not a list": json.RawMessage(`{"users":"anna"}`),
	}
	for name, raw := range cases {
		req := RunRequest{MQTTPassword: "mqtt-geheim", AdminPassword: "admin-geheim", DashboardPrefs: raw}
		if got := req.Secrets(); len(got) != 2 {
			t.Errorf("%s: Secrets() = %q, want only the MQTT and admin password", name, got)
		}
		if seed := req.DashboardSeed(); seed != nil {
			t.Errorf("%s: DashboardSeed() = %s, want nil", name, seed)
		}
	}
}

func TestRunRequestDashboardSeedIsThePrefsPlusVersion(t *testing.T) {
	req := RunRequest{DashboardPrefs: json.RawMessage(`{"theme":"stromblau","users":[{"name":"anna","password":"anna-geheim-1"}]}`)}
	var got map[string]any
	if err := json.Unmarshal(req.DashboardSeed(), &got); err != nil {
		t.Fatalf("seed is not JSON: %v", err)
	}
	if got["version"] != float64(1) {
		t.Errorf("version = %v, want 1", got["version"])
	}
	if got["theme"] != "stromblau" {
		t.Errorf("theme = %v, want stromblau", got["theme"])
	}
	users, ok := got["users"].([]any)
	if !ok || len(users) != 1 {
		t.Errorf("users = %v, want the one user row", got["users"])
	}
}
