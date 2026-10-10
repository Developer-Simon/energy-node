package main

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// Only these elements survive the outline generator in
// scripts/icons/flatten_icons.py. A drawing that reaches for something else
// (text, polyline, a nested svg) would silently vanish from the Home
// Assistant module, so it fails here instead.
var allowedElements = regexp.MustCompile(`<(/?)(path|rect|circle|ellipse|g|line)\b`)

func TestCommittedCatalogueIsCurrent(t *testing.T) {
	want, err := render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	got, err := os.ReadFile(targetPath(mustRepoRoot(t)))
	if err != nil {
		t.Fatalf("read committed catalogue: %v - run (cd dashboard && go run ./cmd/deviceicons)", err)
	}
	if string(got) != string(want) {
		t.Error("icons.source.json has drifted - run (cd dashboard && go run ./cmd/deviceicons)")
	}
}

func TestCatalogueUsesOnlyElementsTheGeneratorUnderstands(t *testing.T) {
	var doc catalogueFile
	if err := json.Unmarshal(mustRender(t), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(doc.Icons) < 19 {
		t.Fatalf("catalogue has %d icons, want the fallback plus the 18 device types", len(doc.Icons))
	}
	for _, icon := range doc.Icons {
		if strings.HasPrefix(icon.HAName, "mdi:") || icon.HAName == "" {
			t.Errorf("%q kept its mdi: prefix as the Home Assistant name", icon.Name)
		}
		for _, tag := range regexp.MustCompile(`<[a-zA-Z/]+`).FindAllString(icon.Markup, -1) {
			if !allowedElements.MatchString(tag + " ") {
				t.Errorf("icon %q uses %q, which the outline generator cannot read", icon.Name, tag)
			}
		}
		if strings.Contains(icon.Markup, "stroke-width") || strings.Contains(icon.Markup, "fill=") {
			t.Errorf("icon %q overrides stroke-width or fill; the generator assumes the shared attributes", icon.Name)
		}
		if len(icon.SHA256) != 64 {
			t.Errorf("icon %q has no source hash", icon.Name)
		}
	}
}

func mustRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func mustRender(t *testing.T) []byte {
	t.Helper()
	data, err := render()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRenderCarriesMDINamesAndAliases(t *testing.T) {
	out, err := render()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Icons []struct {
			Name   string `json:"name"`
			HAName string `json:"ha_name"`
			MDI    string `json:"mdi"`
		} `json:"icons"`
		Aliases []struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"aliases"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	byHA := map[string]string{}
	for _, icon := range doc.Icons {
		if icon.Name != "energy-node:"+icon.HAName {
			t.Errorf("ha_name %q does not match name %q", icon.HAName, icon.Name)
		}
		byHA[icon.HAName] = icon.MDI
	}
	if byHA["wallbox"] != "mdi:ev-station" || byHA["chip"] != "mdi:chip" {
		t.Errorf("mdi names wrong: wallbox=%q chip=%q", byHA["wallbox"], byHA["chip"])
	}
	if len(doc.Aliases) != 11 || doc.Aliases[0].From != "battery-charging" || doc.Aliases[0].To != "charger" {
		t.Errorf("aliases = %+v", doc.Aliases)
	}
	for _, alias := range doc.Aliases {
		if _, ok := byHA[alias.To]; !ok {
			t.Errorf("alias %s -> %s points at no icon", alias.From, alias.To)
		}
		if _, clash := byHA[alias.From]; clash {
			t.Errorf("alias %s shadows a real icon", alias.From)
		}
	}
}
