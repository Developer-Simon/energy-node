package diag

import "testing"

func TestParseReportReadsTheMQTTLogin(t *testing.T) {
	report, err := parseReport([]byte(`{"bundle_version": "v0.2.0", "mqtt": {"login": "denied"}}`))
	if err != nil {
		t.Fatalf("parseReport: %v", err)
	}
	if report.MQTTLogin != "denied" || report.BundleVersion != "v0.2.0" {
		t.Errorf("unexpected report: %+v", report)
	}

	report, err = parseReport([]byte(`{"bundle_version": "v0.2.0"}`))
	if err != nil {
		t.Fatalf("parseReport without mqtt: %v", err)
	}
	if report.MQTTLogin != "" {
		t.Errorf("a report without the mqtt object must leave the login empty, got %q", report.MQTTLogin)
	}
}
