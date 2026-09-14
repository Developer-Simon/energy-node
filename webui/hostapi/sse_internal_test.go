package hostapi

import "testing"

func TestWithAtInsertsTheTimestampAsTheFirstField(t *testing.T) {
	cases := map[string]string{
		`{"id":"10"}`: `{"at":42,"id":"10"}`,
		`{}`:          `{"at":42}`,
		` {"a":1} `:   `{"at":42,"a":1} `,
	}
	for in, want := range cases {
		if got := string(withAt([]byte(in), 42)); got != want {
			t.Errorf("withAt(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestWithAtLeavesNonObjectsAndZeroTimesAlone(t *testing.T) {
	for _, in := range []string{`[1,2]`, `"x"`, `null`} {
		if got := string(withAt([]byte(in), 42)); got != in {
			t.Errorf("withAt(%s) = %s, want it unchanged", in, got)
		}
	}
	if got := string(withAt([]byte(`{"id":"10"}`), 0)); got != `{"id":"10"}` {
		t.Errorf("a zero time must not be written, got %s", got)
	}
}
