package uierror

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorKeepsTheOriginalTextAndTravelsThroughWrapping(t *testing.T) {
	base := New("error.mqtt_rejected.host", "mqtt: host must not contain whitespace", map[string]any{"field": "host"})
	if base.Error() != "mqtt: host must not contain whitespace" {
		t.Fatalf("Error() = %q", base.Error())
	}
	wrapped := fmt.Errorf("save: %w", base)
	got, ok := From(wrapped)
	if !ok || got.Key != "error.mqtt_rejected.host" || got.Params["field"] != "host" {
		t.Fatalf("From(wrapped) = %+v, %v", got, ok)
	}
	if _, ok := From(errors.New("plain")); ok {
		t.Fatal("From(plain) must report false")
	}
	if _, ok := From(nil); ok {
		t.Fatal("From(nil) must report false")
	}
}
