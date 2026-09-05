package settings

import "sort"

// CardType beschreibt, wieviel Platz ein Item-Typ braucht. Einzige Wahrheit -
// ausgespielt als CSS-Custom-Properties in den Inline-Stil der Rasterzelle
// (internal/webui: cardStyle) und ueber GET /api/v1/layout an den Editor.
// Die Zahlen stammen aus Spec E, Abschnitt "Mindestgroessen-Katalog";
// MinSpan ist die dortige Mindestbreite, aufgerundet auf ganze Spuren a 18rem.
//
// MinWidth traegt genau diese Mindestbreite unveraendert weiter: das
// Auswahlfeld im Layout-Editor nennt sie als Grund, wenn es eine
// Groessenklasse sperrt ("Bilanzband braucht mindestens 34rem, das ist
// Spannweite 2"). Sie im JS zu wiederholen waere eine fuenfte Wahrheit.
type CardType struct {
	MinSpan     int    `json:"min_span"`     // Mindest-Spannweite in Rasterspuren
	MinWidth    string `json:"min_width"`    // Mindestbreite aus Spec E, nur fuer die Begruendung im Editor
	MinHeight   string `json:"min_height"`   // CSS-Laenge, wandert unveraendert ins --card-min-height
	FillsHeight bool   `json:"fills_height"` // kann die Karte zusaetzliche Hoehe nutzen?
	DefaultSpan string `json:"default_span"` // Groessenklasse beim Hinzufuegen im Editor
}

// DefaultSpan weicht bei zwei Typen bewusst von MinSpan ab:
//   - energy_board startet bei 3, weil seine Tabelle 38rem braucht und
//     Spannweite 2 nur 36,8rem garantiert.
//   - energy_summary startet bei "full", weil die KPI-Leiste seit Spec E
//     auto-fit ist und ihre Eintraege ueber jede Breite selbst verteilt.
//
// energy_schema fuellt seit der "fliessende Skalierung"-Spec jede Breite
// fluessig aus (kein SVG-min-width mehr) - DefaultSpan == MinSpan wie bei den
// meisten anderen Energiegrafiken. Das *Minimum* bleibt bei der Datentafel
// trotzdem 2: Scrollen ist der in Spec E festgelegte Mechanismus dieser
// Karte - erlaubt, nur nicht die Vorgabe.
var cardCatalog = map[string]CardType{
	"energy_flow":    {MinSpan: 2, MinWidth: "32rem", MinHeight: "25rem", FillsHeight: true, DefaultSpan: "2"},
	"energy_day":     {MinSpan: 2, MinWidth: "30rem", MinHeight: "28rem", FillsHeight: true, DefaultSpan: "2"},
	"energy_band":    {MinSpan: 2, MinWidth: "34rem", MinHeight: "33rem", FillsHeight: true, DefaultSpan: "2"},
	"energy_ring":    {MinSpan: 2, MinWidth: "22rem", MinHeight: "27rem", FillsHeight: false, DefaultSpan: "2"},
	"energy_schema":  {MinSpan: 2, MinWidth: "24rem", MinHeight: "16rem", FillsHeight: false, DefaultSpan: "2"},
	"energy_board":   {MinSpan: 2, MinWidth: "24rem", MinHeight: "14rem", FillsHeight: false, DefaultSpan: "3"},
	"energy_status":  {MinSpan: 1, MinWidth: "14rem", MinHeight: "18rem", FillsHeight: false, DefaultSpan: "1"},
	"energy_summary": {MinSpan: 1, MinWidth: "10rem", MinHeight: "6rem", FillsHeight: false, DefaultSpan: "full"},
	"diagnostics":    {MinSpan: 1, MinWidth: "12rem", MinHeight: "6rem", FillsHeight: false, DefaultSpan: "1"},
	"device":         {MinSpan: 1, MinWidth: "18rem", MinHeight: "10rem", FillsHeight: false, DefaultSpan: "1"},
	"entity_value":   {MinSpan: 1, MinWidth: "10rem", MinHeight: "6rem", FillsHeight: false, DefaultSpan: "1"},
	"entity_group":   {MinSpan: 1, MinWidth: "18rem", MinHeight: "10rem", FillsHeight: true, DefaultSpan: "1"},
	"battery_status": {MinSpan: 1, MinWidth: "16rem", MinHeight: "17rem", FillsHeight: false, DefaultSpan: "1"},
}

// fallbackCardType haelt jeden Aufrufer beantwortbar, auch wenn ein Typ ohne
// Katalogeintrag durchrutscht. Er kann das nicht: TestCardCatalogMatchesSchema
// wirft den Testlauf um, sobald Enum und Katalog auseinanderlaufen. Der
// Fallback ist damit kein Sicherheitsnetz fuer Daten, sondern die Zusage, dass
// cardStyle() nie eine kaputte CSS-Deklaration erzeugt.
var fallbackCardType = CardType{MinSpan: 1, MinWidth: "14rem", MinHeight: "6rem", FillsHeight: false, DefaultSpan: "1"}

// cardVariants haelt Eintraege, die kein *Item-Typ* sind, sondern eine
// Darstellung eines Typs: der Schluessel ist "<typ>:<display>". Sie stehen
// bewusst neben cardCatalog und nicht darin - CardTypeNames() und damit
// TestCardCatalogMatchesSchema sehen nur echte Typen, waehrend CardCatalog()
// beide zusammenfuehrt und der Editor die Variante ueber die card_types-
// Antwort mitbekommt. Die 7rem sind die Hoehe, die compact-card mit ihren bis
// zu drei Zeilen tatsaechlich braucht; die 10rem der Detailkachel liessen
// unter der kompakten Karte ein Drittel Leere stehen.
var cardVariants = map[string]CardType{
	"device:compact": {MinSpan: 1, MinWidth: "14rem", MinHeight: "7rem", FillsHeight: false, DefaultSpan: "1"},
	// Die Trajektorie zeichnet eine Zeitachse ueber zwoelf Stunden. Unter
	// 30rem stehen "jetzt" und die beiden Fensterraender uebereinander -
	// darum ist ihr Minimum 2 und nicht das der Saeule.
	"battery_status:trajectory": {MinSpan: 2, MinWidth: "30rem", MinHeight: "15rem", FillsHeight: false, DefaultSpan: "2"},
}

// cardVariantKey bildet ein Item auf seinen Katalogschluessel ab. Das
// JavaScript des Editors spiegelt diese eine Zeile in cardKey()
// (layout-editor.js) - beide muessen denselben String bilden, sonst rechnet
// der Editor mit einer anderen Mindesthoehe als die Uebersicht.
func cardVariantKey(item Item) string {
	if item.Type == "device" && item.Display == "compact" {
		return "device:compact"
	}
	if item.Type == "battery_status" && item.Display == "trajectory" {
		return "battery_status:trajectory"
	}
	return item.Type
}

// CardTypeForItem ist CardTypeFor mit Blick auf die Darstellung des Items.
func CardTypeForItem(item Item) CardType {
	if variant, ok := cardVariants[cardVariantKey(item)]; ok {
		return variant
	}
	card, _ := CardTypeFor(item.Type)
	return card
}

func CardTypeFor(itemType string) (CardType, bool) {
	card, ok := cardCatalog[itemType]
	if !ok {
		return fallbackCardType, false
	}
	return card, true
}

// CardCatalog liefert eine Kopie: die Tabelle geht ueber die HTTP-API nach
// draussen und darf von dort nicht veraenderbar sein.
func CardCatalog() map[string]CardType {
	clone := make(map[string]CardType, len(cardCatalog)+len(cardVariants))
	for name, card := range cardCatalog {
		clone[name] = card
	}
	for name, card := range cardVariants {
		clone[name] = card
	}
	return clone
}

func CardTypeNames() []string {
	names := make([]string, 0, len(cardCatalog))
	for name := range cardCatalog {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
