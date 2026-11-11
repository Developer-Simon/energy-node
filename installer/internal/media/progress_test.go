package media

import (
	"os"
	"reflect"
	"testing"
)

func parseFile(t *testing.T, path string) []Progress {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var got []Progress
	if err := ParseImagerOutput(f, func(p Progress) { got = append(got, p) }); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestParseImagerOutputOk(t *testing.T) {
	got := parseFile(t, "testdata/imager-ok.txt")
	want := []Progress{
		{Phase: PhasePrepare, Message: "Opening drive..."},
		{Phase: PhasePrepare, Message: "Zero'ing out first and last MB of drive..."},
		{Phase: PhasePrepare, Message: "Opening image file..."},
		{Phase: PhasePrepare, Message: "Starting extraction..."},
		{Phase: PhaseWrite, Percent: 16},
		{Phase: PhaseWrite, Percent: 100},
		{Phase: PhaseVerify, Percent: 21},
		{Phase: PhaseDone},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestParseImagerOutputError(t *testing.T) {
	got := parseFile(t, "testdata/imager-error.txt")
	if len(got) == 0 {
		t.Fatal("no progress events")
	}
	last := got[len(got)-1]
	if last.Phase != PhaseError || last.Message != "Error writing to storage device" {
		t.Errorf("last = %+v", last)
	}
	if got[0].Phase != PhaseWrite || got[0].Percent != 45 {
		t.Errorf("first = %+v, want write 45", got[0])
	}
}
