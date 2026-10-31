package hostapi_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

func TestSaveFileHandsTheNameAndTextToTheHost(t *testing.T) {
	var gotName, gotContent string
	server, _ := newTestServer(t, func(o *hostapi.Options) {
		o.SaveFile = func(name string, content []byte) (string, error) {
			gotName, gotContent = name, string(content)
			return "/home/dev/Downloads/" + name, nil
		}
	})
	rec := do(t, server, http.MethodPost, "/api/save-file", `{"filename":"energy-node-protokoll.txt","content":"zeile 1\nzeile 2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if gotName != "energy-node-protokoll.txt" || gotContent != "zeile 1\nzeile 2" {
		t.Errorf("host got %q / %q", gotName, gotContent)
	}
	if !strings.Contains(rec.Body.String(), "/home/dev/Downloads/energy-node-protokoll.txt") {
		t.Errorf("response lacks the path: %s", rec.Body.String())
	}
	boot := do(t, server, http.MethodGet, "/api/bootstrap", "")
	if !strings.Contains(boot.Body.String(), `"can_save_file":true`) {
		t.Errorf("bootstrap does not advertise the saver: %s", boot.Body.String())
	}
}

func TestSaveFileReportsAHostError(t *testing.T) {
	server, _ := newTestServer(t, func(o *hostapi.Options) {
		o.SaveFile = func(string, []byte) (string, error) { return "", errors.New("disk full") }
	})
	rec := do(t, server, http.MethodPost, "/api/save-file", `{"filename":"a.txt","content":"x"}`)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "FILE_SAVE_FAILED") {
		t.Fatalf("got %d %s, want 500 FILE_SAVE_FAILED", rec.Code, rec.Body.String())
	}
}

func TestSaveFileIsNotSupportedWithoutAHostSaver(t *testing.T) {
	server, _ := newTestServer(t, nil)
	rec := do(t, server, http.MethodPost, "/api/save-file", `{"filename":"a.txt","content":"x"}`)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
	boot := do(t, server, http.MethodGet, "/api/bootstrap", "")
	if strings.Contains(boot.Body.String(), "can_save_file") {
		t.Errorf("bootstrap advertises a saver that is not there")
	}
}
