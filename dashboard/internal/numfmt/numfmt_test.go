package numfmt

import (
	"encoding/json"
	"os"
	"testing"
)

type fixtureCase struct {
	Name     string    `json:"name"`
	Format   string    `json:"format"`
	Grouping string    `json:"grouping"`
	Catalog  [2]string `json:"catalog"`
	Kind     string    `json:"kind"`
	Raw      string    `json:"raw"`
	Unit     string    `json:"unit"`
	Number   float64   `json:"number"`
	Decimals int       `json:"decimals"`
	Want     string    `json:"want"`
}

// The same file drives dashboard/test/i18n-format.test.mjs, so the server's
// first render and the browser's live updates format identically.
func TestSharedCases(t *testing.T) {
	data, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []fixtureCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			style := Resolve(c.Format, c.Grouping, c.Catalog[0], c.Catalog[1])
			var got string
			switch c.Kind {
			case "value":
				got = style.Value(c.Raw, c.Unit)
			case "number":
				got = style.Number(c.Number, c.Decimals)
			default:
				t.Fatalf("unknown kind %q", c.Kind)
			}
			if got != c.Want {
				t.Fatalf("got %q, want %q", got, c.Want)
			}
		})
	}
}

func TestNumberRejectsNonFinite(t *testing.T) {
	style := Resolve(FormatComma, GroupingMatch, "", "")
	for _, value := range []float64{nan(), inf()} {
		if got := style.Number(value, 1); got != "" {
			t.Fatalf("Number(%v) = %q, want empty", value, got)
		}
	}
}

func nan() float64 { zero := 0.0; return zero / zero }
func inf() float64 { zero := 0.0; return 1 / zero }
