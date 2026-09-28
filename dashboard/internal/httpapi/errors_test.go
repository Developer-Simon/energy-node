package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/uierror"
)

func decodeErrorBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON %q: %v", rec.Body.String(), err)
	}
	return body
}

func TestWriteErrorKeepsTheTwoFieldShape(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, http.StatusNotFound, "device_not_found", "Gerät wurde nicht gefunden")
	want := map[string]any{"code": "device_not_found", "message": "Gerät wurde nicht gefunden"}
	if got := decodeErrorBody(t, rec); !reflect.DeepEqual(got, want) || rec.Code != http.StatusNotFound {
		t.Fatalf("got %d %v", rec.Code, got)
	}
}

func TestWriteErrorKeyAddsKeyAndParams(t *testing.T) {
	rec := httptest.NewRecorder()
	writeErrorKey(rec, http.StatusForbidden, "bridge_forbidden", "error.bridge_forbidden.revisions", nil, "Für Bridge-Revisionen fehlt die Berechtigung")
	got := decodeErrorBody(t, rec)
	if got["message_key"] != "error.bridge_forbidden.revisions" || got["message"] != "Für Bridge-Revisionen fehlt die Berechtigung" {
		t.Fatalf("got %v", got)
	}
	if _, present := got["params"]; present {
		t.Fatalf("nil params must be omitted: %v", got)
	}
}

func TestWriteErrorDetailPassesPlainErrorsAsDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	writeErrorDetail(rec, http.StatusBadRequest, "settings_rejected", errors.New("layout: page id is empty"))
	want := map[string]any{"code": "settings_rejected", "message": "layout: page id is empty", "detail": "layout: page id is empty"}
	if got := decodeErrorBody(t, rec); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestWriteErrorDetailUsesTheKeyOfATypedError(t *testing.T) {
	rec := httptest.NewRecorder()
	typed := uierror.New("error.bridge_rejected.max_connections", "bridge: only 1 connection(s) are supported today", map[string]any{"max": 1})
	writeErrorDetail(rec, http.StatusBadRequest, "bridge_rejected", fmt.Errorf("save: %w", typed))
	got := decodeErrorBody(t, rec)
	if got["message_key"] != "error.bridge_rejected.max_connections" || got["message"] != "save: bridge: only 1 connection(s) are supported today" {
		t.Fatalf("got %v", got)
	}
	if params, _ := got["params"].(map[string]any); params["max"] != float64(1) {
		t.Fatalf("params = %v", got["params"])
	}
	if _, present := got["detail"]; present {
		t.Fatalf("a typed error needs no detail: %v", got)
	}
}

func TestWriteTinyTuyaErrorKeepsToolOutput(t *testing.T) {
	rec := httptest.NewRecorder()
	writeTinyTuyaError(rec, http.StatusBadGateway, "tiny_tuya_rejected", errors.New("wizard failed"))
	got := decodeErrorBody(t, rec)
	if got["tool_output"] != "wizard failed" || got["detail"] != "wizard failed" || got["message"] != "wizard failed" {
		t.Fatalf("got %v", got)
	}
}
