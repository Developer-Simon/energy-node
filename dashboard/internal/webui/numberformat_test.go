package webui

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/registry"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

func renderValueCard(t *testing.T, lang string, entity registry.EntityView) string {
	t.Helper()
	var out bytes.Buffer
	if err := overviewSets.get(lang).ExecuteTemplate(&out, "entity-value-card", entity); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func energyEntity(value, unit string) registry.EntityView {
	return registry.EntityView{UniqueID: "sensor.e", Name: "Energie", Component: "sensor", UnitOfMeasurement: unit, Value: value, HasValue: true}
}

// The number format is global: "comma" wins over an English UI, and "auto"
// takes the separators from the active language's catalog.
func TestFormatValueFollowsTheSettingNotTheLanguage(t *testing.T) {
	t.Cleanup(func() { setNumberSettings(settings.Default()) })

	value := settings.Default()
	value.NumberFormat = "comma"
	setNumberSettings(value)
	if body := renderValueCard(t, "en", energyEntity("12345.6", "kWh")); !strings.Contains(body, "12.345,6") {
		t.Fatalf("comma setting ignored in English: %s", body)
	}

	setNumberSettings(settings.Default())
	if body := renderValueCard(t, "de", energyEntity("12345.6", "kWh")); !strings.Contains(body, "12.345,6") {
		t.Fatalf("auto did not use the German catalog: %s", body)
	}
	if body := renderValueCard(t, "en", energyEntity("12345.6", "kWh")); !strings.Contains(body, "12,345.6") {
		t.Fatalf("auto did not use the English catalog: %s", body)
	}
}

func TestFormatValueLeavesUnitlessValuesRaw(t *testing.T) {
	t.Cleanup(func() { setNumberSettings(settings.Default()) })
	value := settings.Default()
	value.NumberFormat = "comma"
	setNumberSettings(value)
	if body := renderValueCard(t, "de", energyEntity("10342.5", "")); !strings.Contains(body, "10342.5") {
		t.Fatalf("unitless value was formatted: %s", body)
	}
}

func TestOverviewRendersTheNumberFormatMetaTags(t *testing.T) {
	store := settings.NewStore(t.TempDir())
	value := settings.Default()
	value.NumberFormat = "point"
	value.NumberGrouping = "thin"
	if err := store.SaveSettings(value); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { setNumberSettings(settings.Default()) })
	recorder := httptest.NewRecorder()
	Overview(registry.New(), nil, store).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	body := recorder.Body.String()
	for _, want := range []string{`<meta name="number-format" content="point">`, `<meta name="number-grouping" content="thin">`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}

func TestHistoryPanelLoadsTheFlatpickrLocaleOfTheLanguage(t *testing.T) {
	render := func(lang string) string {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.AddCookie(&http.Cookie{Name: "lang", Value: lang})
		recorder := httptest.NewRecorder()
		Overview(registry.New(), nil, nil).ServeHTTP(recorder, request)
		return recorder.Body.String()
	}
	if body := render("de"); !strings.Contains(body, "/static/js-deps/flatpickr-l10n-de.js?v=1,") {
		t.Fatal("German page does not load the German flatpickr locale")
	}
	if body := render("en"); strings.Contains(body, "flatpickr-l10n-") {
		t.Fatal("English page loads a flatpickr locale although flatpickr's default is English")
	}
}

func TestSettingsPageOffersTheNumberFormat(t *testing.T) {
	var out bytes.Buffer
	if err := overviewSets.get("en").ExecuteTemplate(&out, "settings", map[string]any{"InstalledServices": map[string]bool{}}); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	for _, want := range []string{`name="number-format" value="auto"`, `name="number-format" value="comma"`, `name="number-format" value="point"`, `name="number-grouping" value="thin"`, "Number format", "Digit grouping"} {
		if !strings.Contains(body, want) {
			t.Fatalf("settings page lacks %s", want)
		}
	}
}
