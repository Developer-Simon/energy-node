package settings

import (
	"encoding/json"
	"testing"
)

// TestCardCatalogMatchesSchema haelt cardCatalog und den type-Enum von
// layout.schema.json deckungsgleich. Ein zwoelfter Item-Typ ohne
// Katalogeintrag soll den Testlauf umwerfen, nicht die Uebersicht.
func TestCardCatalogMatchesSchema(t *testing.T) {
	enum := layoutSchemaItemTypes(t)
	if len(enum) == 0 {
		t.Fatal("kein type-Enum in layout.schema.json gefunden")
	}
	for _, itemType := range enum {
		if _, ok := CardTypeFor(itemType); !ok {
			t.Errorf("Schema-Typ %q hat keinen Katalogeintrag", itemType)
		}
	}
	inEnum := map[string]bool{}
	for _, itemType := range enum {
		inEnum[itemType] = true
	}
	for _, itemType := range CardTypeNames() {
		if !inEnum[itemType] {
			t.Errorf("Katalogtyp %q steht nicht im Schema-Enum", itemType)
		}
	}
}

// TestCardCatalogValues sichert die Werte ab, die im Spec eine eigene
// Begruendung haben - sie sind der Grund, warum DefaultSpan ueberhaupt
// getrennt von MinSpan existiert.
func TestCardCatalogValues(t *testing.T) {
	for _, testCase := range []struct {
		itemType    string
		minSpan     int
		defaultSpan string
	}{
		{"energy_schema", 2, "2"},
		{"energy_board", 2, "3"},
		{"energy_summary", 1, "full"},
		{"device", 1, "1"},
		{"energy_band", 2, "2"},
		{"entity_value", 1, "1"},
		{"entity_group", 1, "1"},
	} {
		card, ok := CardTypeFor(testCase.itemType)
		if !ok {
			t.Fatalf("%s fehlt im Katalog", testCase.itemType)
		}
		if card.MinSpan != testCase.minSpan || card.DefaultSpan != testCase.defaultSpan {
			t.Errorf("%s = MinSpan %d/DefaultSpan %q, want %d/%q",
				testCase.itemType, card.MinSpan, card.DefaultSpan, testCase.minSpan, testCase.defaultSpan)
		}
	}
}

func TestCardTypeForUnknownReturnsUsableFallback(t *testing.T) {
	card, ok := CardTypeFor("gibt-es-nicht")
	if ok {
		t.Fatal("unbekannter Typ wurde als bekannt gemeldet")
	}
	if card.MinSpan < 1 || card.MinHeight == "" || card.DefaultSpan == "" {
		t.Fatalf("Fallback ist unbrauchbar: %#v", card)
	}
}

// layoutSchemaItemTypes greift den type-Enum aus dem eingebetteten Schema.
// Ueber map[string]any statt ueber ein Spiegel-Struct: der Pfad steht damit
// genau einmal, hier, und nicht zusaetzlich als Typdefinition.
func layoutSchemaItemTypes(t *testing.T) []string {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(layoutSchema, &document); err != nil {
		t.Fatalf("layout.schema.json ist kein JSON: %v", err)
	}
	node := document
	for _, step := range []string{
		"properties", "pages", "items", "properties", "groups", "items",
		"properties", "items", "items", "properties", "type",
	} {
		next, ok := node[step].(map[string]any)
		if !ok {
			t.Fatalf("Schema-Pfad bricht bei %q ab", step)
		}
		node = next
	}
	raw, ok := node["enum"].([]any)
	if !ok {
		t.Fatal("type-Knoten hat kein enum")
	}
	result := make([]string, 0, len(raw))
	for _, value := range raw {
		name, ok := value.(string)
		if !ok {
			t.Fatalf("enum-Eintrag ist keine Zeichenkette: %#v", value)
		}
		result = append(result, name)
	}
	return result
}

// Die kompakte Geraetekachel ist kein eigener Item-Typ (das Layout kennt
// weiter nur "device"), aber sie braucht eine eigene Mindesthoehe: 10rem
// waeren fuer drei Zeilen Luft statt Inhalt. Der Varianteneintrag liefert
// sie ueber dieselbe card_types-Antwort an den Editor - deshalb steht er
// in CardCatalog(), aber nicht in CardTypeNames(), wo ihn
// TestCardCatalogMatchesSchema gegen den Schema-Enum halten wuerde.
func TestCardCatalogHasCompactDeviceVariant(t *testing.T) {
	catalog := CardCatalog()
	variant, ok := catalog["device:compact"]
	if !ok {
		t.Fatal("device:compact fehlt in CardCatalog()")
	}
	if variant.MinHeight != "7rem" || variant.MinSpan != 1 || variant.DefaultSpan != "1" {
		t.Errorf("device:compact = %+v, want MinHeight 7rem / MinSpan 1 / DefaultSpan 1", variant)
	}
	for _, name := range CardTypeNames() {
		if name == "device:compact" {
			t.Error("device:compact steht in CardTypeNames() und wuerde gegen den Schema-Enum gehalten")
		}
	}
}

func TestCardTypeForItemPicksTheDisplayVariant(t *testing.T) {
	compact := CardTypeForItem(Item{Type: "device", Display: "compact"})
	if compact.MinHeight != "7rem" {
		t.Errorf("kompakte Geraetekachel = MinHeight %q, want 7rem", compact.MinHeight)
	}
	detail := CardTypeForItem(Item{Type: "device", Display: "detail"})
	if detail.MinHeight != "10rem" {
		t.Errorf("Detailkachel = MinHeight %q, want 10rem", detail.MinHeight)
	}
	// Leeres Display heisst "nicht normalisiert" - und damit Detail.
	if CardTypeForItem(Item{Type: "device"}).MinHeight != "10rem" {
		t.Error("device ohne display faellt nicht auf die Detailkachel zurueck")
	}
	if CardTypeForItem(Item{Type: "energy_ring", Display: "compact"}).MinHeight != "27rem" {
		t.Error("display wirkt auf einen fremden Typ")
	}
}

func TestCardCatalogHasBatteryStatusAndTrajectoryVariant(t *testing.T) {
	catalog := CardCatalog()
	column, ok := catalog["battery_status"]
	if !ok {
		t.Fatal("CardCatalog enthaelt keinen Eintrag battery_status")
	}
	if column.MinSpan != 1 || column.DefaultSpan != "1" {
		t.Fatalf("battery_status = %#v, want MinSpan 1 / DefaultSpan \"1\"", column)
	}
	trajectory, ok := catalog["battery_status:trajectory"]
	if !ok {
		t.Fatal("CardCatalog enthaelt keine Variante battery_status:trajectory")
	}
	if trajectory.MinSpan != 2 || trajectory.DefaultSpan != "2" {
		t.Fatalf("battery_status:trajectory = %#v, want MinSpan 2 / DefaultSpan \"2\"", trajectory)
	}
}

func TestCardTypeForItemPicksTheTrajectoryVariant(t *testing.T) {
	if got := CardTypeForItem(Item{Type: "battery_status", Display: "column"}).MinSpan; got != 1 {
		t.Fatalf("Saeule MinSpan = %d, want 1", got)
	}
	if got := CardTypeForItem(Item{Type: "battery_status", Display: "trajectory"}).MinSpan; got != 2 {
		t.Fatalf("Trajektorie MinSpan = %d, want 2", got)
	}
}
