package storagehealth

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestCatalogKeysAndTexts verifies that storagehealth functions provide
// catalog keys, and that the German text with params substituted matches
// the expected label.
func TestCatalogKeysAndTexts(t *testing.T) {
	// Read de.json from ../webui/catalogs/
	data, err := os.ReadFile("../webui/catalogs/de.json")
	if err != nil {
		t.Fatalf("failed to read de.json: %v", err)
	}
	de := map[string]string{}
	if err := json.Unmarshal(data, &de); err != nil {
		t.Fatalf("failed to parse de.json: %v", err)
	}

	// Test lifeTime codes 1-11
	for code := 1; code <= 11; code++ {
		lifetime, valid := lifeTime(code)
		if !valid {
			t.Fatalf("lifeTime code %d should be valid", code)
		}
		if lifetime.LabelKey == "" {
			t.Errorf("lifeTime code %d: LabelKey is empty", code)
			continue
		}
		if _, ok := de[lifetime.LabelKey]; !ok {
			t.Errorf("lifeTime code %d: key %q not found in de.json", code, lifetime.LabelKey)
			continue
		}
		// Substitute params and check text matches
		catalogText := de[lifetime.LabelKey]
		expectedLabel := substituteParams(catalogText, lifetime.LabelParams)
		if expectedLabel != lifetime.Label {
			t.Errorf("lifeTime code %d: expected label %q, got %q", code, lifetime.Label, expectedLabel)
		}
	}

	// Test preEOL codes 1-3
	for code := 1; code <= 3; code++ {
		preeol, valid := preEOL(code)
		if !valid {
			t.Fatalf("preEOL code %d should be valid", code)
		}
		if preeol.LabelKey == "" {
			t.Errorf("preEOL code %d: LabelKey is empty", code)
			continue
		}
		if _, ok := de[preeol.LabelKey]; !ok {
			t.Errorf("preEOL code %d: key %q not found in de.json", code, preeol.LabelKey)
			continue
		}
		// Substitute params and check text matches
		catalogText := de[preeol.LabelKey]
		expectedLabel := substituteParams(catalogText, preeol.LabelParams)
		if expectedLabel != preeol.Label {
			t.Errorf("preEOL code %d: expected label %q, got %q", code, preeol.Label, expectedLabel)
		}
	}

	// Test remainingLabel with sample values (days < 365 and >= 365)
	testCases := []struct {
		name    string
		minDays float64
		maxDays float64
	}{
		{"days", 100, 200},
		{"days large", 300, 400},
		{"years", 400, 900},
	}
	for _, tc := range testCases {
		result := remainingLabel(tc.minDays, tc.maxDays)
		if result.Key == "" {
			t.Errorf("remainingLabel(%v, %v): Key is empty", tc.minDays, tc.maxDays)
			continue
		}
		if _, ok := de[result.Key]; !ok {
			t.Errorf("remainingLabel(%v, %v): key %q not found in de.json", tc.minDays, tc.maxDays, result.Key)
			continue
		}
		catalogText := de[result.Key]
		expectedLabel := substituteParams(catalogText, result.Params)
		if expectedLabel != result.Label {
			t.Errorf("remainingLabel(%v, %v): expected %q, got %q", tc.minDays, tc.maxDays, result.Label, expectedLabel)
		}
	}

	// Test consumedPercent with sample values
	consumedTestCases := []struct {
		name              string
		hostWritesBytes   float64
		enduranceMinBytes float64
		enduranceMaxBytes float64
	}{
		{"normal", 500_000_000_000, 1_000_000_000_000, 3_000_000_000_000},
		{"zero", 0, 1_000_000_000_000, 3_000_000_000_000},
	}
	for _, tc := range consumedTestCases {
		_, _, label, key, params := consumedPercent(tc.hostWritesBytes, tc.enduranceMinBytes, tc.enduranceMaxBytes)
		if key == "" {
			// Empty key is valid for zero bytes
			if label != "" {
				t.Errorf("consumedPercent(%v, ...): expected empty label for empty key, got %q", tc.hostWritesBytes, label)
			}
			continue
		}
		if _, ok := de[key]; !ok {
			t.Errorf("consumedPercent(%v, ...): key %q not found in de.json", tc.hostWritesBytes, key)
			continue
		}
		catalogText := de[key]
		expectedLabel := substituteParams(catalogText, params)
		if expectedLabel != label {
			t.Errorf("consumedPercent(%v, ...): expected %q, got %q", tc.hostWritesBytes, label, expectedLabel)
		}
	}

	// Test no_mmc reason key
	if _, ok := de["storage_health.reason.no_mmc"]; !ok {
		t.Error("storage_health.reason.no_mmc key not found in de.json")
	}
}

// substituteParams replaces {key} placeholders with values from the params map.
func substituteParams(text string, params map[string]any) string {
	for key, value := range params {
		placeholder := "{" + key + "}"
		switch v := value.(type) {
		case int:
			text = strings.ReplaceAll(text, placeholder, fmt.Sprintf("%d", v))
		case float64:
			text = strings.ReplaceAll(text, placeholder, fmt.Sprintf("%.1f", v))
		default:
			text = strings.ReplaceAll(text, placeholder, fmt.Sprintf("%v", v))
		}
	}
	return text
}
