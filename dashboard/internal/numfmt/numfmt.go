// Package numfmt formats numbers for display with the separators the
// operator chose in the settings (number_format, number_grouping). Value
// never rounds: it only re-punctuates the digits a device reported, so the
// dashboard shows exactly the precision the device sent. The browser
// counterpart lives in static/js/i18n.js; both run testdata/cases.json.
package numfmt

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	FormatAuto  = "auto"
	FormatComma = "comma"
	FormatPoint = "point"

	GroupingMatch = "match"
	GroupingThin  = "thin"

	// ThinSpace is U+202F NARROW NO-BREAK SPACE, the SI digit-group
	// separator. It never lets a number wrap across lines.
	ThinSpace = " "

	// MinGroupDigits is the shortest integer part that gets grouped:
	// "1234 W" stays compact, "12.345 W" is grouped.
	MinGroupDigits = 5
)

// Style is a resolved pair of separators.
type Style struct {
	Decimal string
	Group   string
}

var plainDecimal = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

// Resolve turns the two settings into separators. catalogDecimal and
// catalogGroup are the active language's meta.number.decimal and
// meta.number.group and only matter for FormatAuto (and unknown formats).
// A missing catalog entry arrives as the key itself and is rejected by the
// single-character check, which falls back to a point.
func Resolve(format, grouping, catalogDecimal, catalogGroup string) Style {
	style := Style{Decimal: ".", Group: ","}
	switch format {
	case FormatComma:
		style = Style{Decimal: ",", Group: "."}
	case FormatPoint:
	default:
		if single(catalogDecimal) && single(catalogGroup) && catalogDecimal != catalogGroup {
			style = Style{Decimal: catalogDecimal, Group: catalogGroup}
		}
	}
	if grouping == GroupingThin {
		style.Group = ThinSpace
	}
	return style
}

func single(value string) bool { return utf8.RuneCountInString(value) == 1 }

// Value formats a raw device value. Only a plain decimal ("-12.50") that
// carries a unit is touched. Text states, exponents and unitless numbers
// (counters, years, IDs) come back unchanged.
func (s Style) Value(raw, unit string) string {
	if strings.TrimSpace(unit) == "" || !plainDecimal.MatchString(raw) {
		return raw
	}
	return s.digits(raw)
}

// Number formats value with a fixed number of decimals. NaN and infinities
// yield "", callers show their own placeholder.
func (s Style) Number(value float64, decimals int) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return ""
	}
	text := strconv.FormatFloat(value, 'f', decimals, 64)
	if strings.HasPrefix(text, "-") && strings.Trim(text, "-0.") == "" {
		text = text[1:]
	}
	return s.digits(text)
}

func (s Style) digits(text string) string {
	sign := ""
	if strings.HasPrefix(text, "-") {
		sign, text = "-", text[1:]
	}
	whole, fraction, hasFraction := strings.Cut(text, ".")
	if len(whole) >= MinGroupDigits {
		var grouped strings.Builder
		for i, digit := range whole {
			if i > 0 && (len(whole)-i)%3 == 0 {
				grouped.WriteString(s.Group)
			}
			grouped.WriteRune(digit)
		}
		whole = grouped.String()
	}
	if hasFraction {
		return sign + whole + s.Decimal + fraction
	}
	return sign + whole
}
