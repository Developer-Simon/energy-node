package webui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Shared building blocks of the device icon catalogue. An icon family (all
// batteries, all wallboxes, every icon with a lightning bolt) composes its
// drawings from these instead of drawing each part again, so the parts look
// the same everywhere. Before, the catalogue had eight different bolts.

// num formats a coordinate with at most two decimals, the precision of the
// hand-drawn paths.
func num(v float64) string {
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
}

// boltPoints is the catalogue's lightning bolt, 16 units tall and point
// symmetric around (12, 12), so it sits optically centred at any size.
var boltPoints = [6][2]float64{{13.6, 4}, {7.6, 13}, {11.4, 13}, {10.4, 20}, {16.4, 11}, {12.6, 11}}

// bolt returns the bolt centred on (cx, cy) with height h.
func bolt(cx, cy, h float64) string {
	s := h / 16
	parts := make([]string, len(boltPoints))
	for i, p := range boltPoints {
		parts[i] = num(cx+s*(p[0]-12)) + " " + num(cy+s*(p[1]-12))
	}
	return `<path d="M` + strings.Join(parts, " ") + `Z"/>`
}

func polar(cx, cy, r, deg float64) (float64, float64) {
	a := deg * math.Pi / 180
	return cx + r*math.Cos(a), cy + r*math.Sin(a)
}

// rotor returns three curved fan blades around (cx, cy) reaching radius r,
// used by the heat pump and the fan.
func rotor(cx, cy, r float64) string {
	var b strings.Builder
	for _, th := range []float64{-90, 30, 150} {
		p1x, p1y := polar(cx, cy, r*0.8, th+40)
		tx, ty := polar(cx, cy, r, th)
		p2x, p2y := polar(cx, cy, r*0.5, th-22)
		fmt.Fprintf(&b, `<path d="M%s %sQ%s %s %s %sQ%s %s %s %sZ"/>`,
			num(cx), num(cy), num(p1x), num(p1y), num(tx), num(ty), num(p2x), num(p2y), num(cx), num(cy))
	}
	return b.String()
}

const (
	// batteryBody is a horizontal battery with an attached pole, its ink
	// centred on the 24 grid. Variants only add content inside.
	batteryBody = `<rect x="2.5" y="7" width="16.5" height="10" rx="2.5"/><path d="M19 10h1.5a1 1 0 0 1 1 1v2a1 1 0 0 1-1 1H19"/>`
	// houseBody is roof and walls, a door or a bolt goes inside.
	houseBody = `<path d="M3.5 11.5 12 4l8.5 7.5"/><path d="M5.5 10v10.5h13V10"/>`
	// plugHead is a two-pin plug seen from the side, cable end at y 16.
	plugHead = `<path d="M9 3v5M15 3v5"/><path d="M7 8h10v3a5 5 0 0 1-10 0z"/>`
	// wallboxBody is the wall unit of the wallbox, combined with
	// bolt(8.5, 10.5, 7.5).
	wallboxBody = `<rect x="3" y="3.5" width="11" height="14" rx="2.5"/>`
)
