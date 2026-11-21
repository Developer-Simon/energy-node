package autolearn

import "testing"

func TestDocLocationPresent(t *testing.T) {
	doc, err := ParseDoc([]byte(`{"rules":[],"settings":{"latitude":52.5,"longitude":13.4}}`))
	if err != nil {
		t.Fatalf("ParseDoc: %v", err)
	}
	if doc.Location == nil {
		t.Fatal("Location = nil, want 52.5/13.4")
	}
	if doc.Location.Lat != 52.5 || doc.Location.Lon != 13.4 {
		t.Errorf("Location = %+v, want Lat 52.5 Lon 13.4", *doc.Location)
	}
}

func TestDocLocationMissing(t *testing.T) {
	for name, raw := range map[string]string{
		"no settings":    `{"rules":[]}`,
		"latitude only":  `{"settings":{"latitude":52.5}}`,
		"longitude only": `{"settings":{"longitude":13.4}}`,
		"both zero":      `{"settings":{"latitude":0,"longitude":0}}`,
	} {
		doc, err := ParseDoc([]byte(raw))
		if err != nil {
			t.Fatalf("%s: ParseDoc: %v", name, err)
		}
		if doc.Location != nil {
			t.Errorf("%s: Location = %+v, want nil", name, *doc.Location)
		}
	}
}
