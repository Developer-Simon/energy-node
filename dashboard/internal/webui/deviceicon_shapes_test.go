package webui

import (
	"strings"
	"testing"
)

func TestBoltIsTheCanonicalShape(t *testing.T) {
	if got, want := bolt(12, 12, 16), `<path d="M13.6 4 7.6 13 11.4 13 10.4 20 16.4 11 12.6 11Z"/>`; got != want {
		t.Errorf("bolt(12,12,16) = %s, want %s", got, want)
	}
	if got, want := bolt(12, 9.4, 6.6), `<path d="M12.66 6.1 10.19 9.81 11.75 9.81 11.34 12.7 13.82 8.99 12.25 8.99Z"/>`; got != want {
		t.Errorf("bolt(12,9.4,6.6) = %s, want %s", got, want)
	}
}

func TestRotorDrawsThreeBlades(t *testing.T) {
	want := `<path d="M12 12Q15.19 8.2 12 5.8Q10.84 9.13 12 12Z"/>` +
		`<path d="M12 12Q13.7 16.66 17.37 15.1Q15.07 12.43 12 12Z"/>` +
		`<path d="M12 12Q7.12 11.14 6.63 15.1Q10.09 14.44 12 12Z"/>`
	if got := rotor(12, 12, 6.2); got != want {
		t.Errorf("rotor(12,12,6.2) = %s, want %s", got, want)
	}
}

func TestNumRoundsToTwoDecimals(t *testing.T) {
	for in, want := range map[float64]string{12: "12", 12.346: "12.35", 0.1: "0.1", 7.6049: "7.6"} {
		if got := num(in); got != want {
			t.Errorf("num(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestShapesAreStrokeOnly(t *testing.T) {
	for name, shape := range map[string]string{"battery": batteryBody, "house": houseBody, "plug": plugHead, "wallbox": wallboxBody} {
		if strings.Contains(shape, "fill") || strings.Contains(shape, "stroke") {
			t.Errorf("%s carries paint attributes: %s", name, shape)
		}
	}
}
