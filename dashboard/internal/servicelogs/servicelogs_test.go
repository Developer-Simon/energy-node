package servicelogs

import (
	"errors"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	invalid := map[string]Query{
		"unknown unit":      {Unit: "sshd", SinceMinutes: 60, Lines: 200},
		"unknown since":     {Unit: "dashboard", SinceMinutes: 7, Lines: 200},
		"unknown lines":     {Unit: "dashboard", SinceMinutes: 60, Lines: 5000},
		"cursor without s=": {Unit: "dashboard", SinceMinutes: 60, Lines: 200, Cursor: "i=1"},
		"filter too long":   {Unit: "dashboard", SinceMinutes: 60, Lines: 200, Filter: strings.Repeat("ä", 201)},
	}
	for name, q := range invalid {
		if err := q.Validate(); !errors.Is(err, ErrInvalidQuery) {
			t.Errorf("%s: Validate() = %v, want ErrInvalidQuery", name, err)
		}
	}

	valid := Query{Unit: "dashboard", SinceMinutes: 60, Lines: 200}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() of valid query = %v, want nil", err)
	}
}
