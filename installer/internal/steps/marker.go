// Package steps runs a bundle's bootstrap steps against a node: it filters
// the manifest's step list through a Selection, invokes each step over the
// held SSH connection with its EN_* environment inlined, and turns the
// "##STEP ..." marker stream on stdout into typed progress events.
package steps

import "strings"

// Kind identifies which of the four marker forms a line is.
type Kind int

const (
	Begin Kind = iota
	OK
	Skip
	Fail
)

func (k Kind) String() string {
	switch k {
	case Begin:
		return "begin"
	case OK:
		return "ok"
	case Skip:
		return "skip"
	case Fail:
		return "fail"
	default:
		return "unknown"
	}
}

// Marker is one parsed "##STEP <id> <kind> [detail]" line. Detail carries
// the skip reason for Skip and the fault code for Fail; it is empty for
// Begin and OK.
type Marker struct {
	StepID string
	Kind   Kind
	Detail string
}

// ParseMarker parses one line of a step script's stdout. ok is false for
// any line that is not a marker -- human text, which callers forward
// unparsed rather than discard: E9 only translates what the interface
// shows the operator, not the log.
func ParseMarker(line string) (Marker, bool) {
	const prefix = "##STEP "
	if !strings.HasPrefix(line, prefix) {
		return Marker{}, false
	}
	fields := strings.SplitN(strings.TrimPrefix(line, prefix), " ", 3)
	if len(fields) < 2 {
		return Marker{}, false
	}
	var kind Kind
	switch fields[1] {
	case "begin":
		kind = Begin
	case "ok":
		kind = OK
	case "skip":
		kind = Skip
	case "fail":
		kind = Fail
	default:
		return Marker{}, false
	}
	detail := ""
	if len(fields) == 3 {
		detail = fields[2]
	}
	return Marker{StepID: fields[0], Kind: kind, Detail: detail}, true
}
