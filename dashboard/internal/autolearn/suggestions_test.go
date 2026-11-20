package autolearn

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func suggestionTestState(t *testing.T) (*SuggestionState, string, *[]string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "autolearn", "suggestions.json")
	var logs []string
	s := LoadSuggestionState(path, func(format string, args ...any) {
		logs = append(logs, format)
	})
	return s, path, &logs
}

func suggestionFinding(id, device string) Finding {
	return Finding{ID: id, Device: device}
}

func TestSuggestionLaterHidesSevenDays(t *testing.T) {
	s, _, _ := suggestionTestState(t)
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.Local)
	all := []Finding{suggestionFinding("a", "dev1")}

	if err := s.Later("a", now); err != nil {
		t.Fatalf("Later: %v", err)
	}
	if got := s.Visible(all, now.Add(6*24*time.Hour+23*time.Hour), nil); len(got) != 0 {
		t.Fatalf("visible 6d23h after Later = %d, want 0", len(got))
	}
	if got := s.Visible(all, now.Add(7*24*time.Hour), nil); len(got) != 1 {
		t.Fatalf("visible 7d after Later = %d, want 1", len(got))
	}
}

func TestSuggestionUndoRestoresDismissedAndLater(t *testing.T) {
	s, _, _ := suggestionTestState(t)
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.Local)
	all := []Finding{suggestionFinding("a", "dev1"), suggestionFinding("b", "dev1")}

	if err := s.Dismiss("a", now); err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	if err := s.Later("b", now); err != nil {
		t.Fatalf("Later: %v", err)
	}
	if got := s.Visible(all, now, nil); len(got) != 0 {
		t.Fatalf("visible after Dismiss and Later = %d, want 0", len(got))
	}

	if err := s.Undo("a"); err != nil {
		t.Fatalf("Undo a: %v", err)
	}
	if err := s.Undo("b"); err != nil {
		t.Fatalf("Undo b: %v", err)
	}
	if got := s.Visible(all, now, nil); len(got) != 2 {
		t.Fatalf("visible after Undo = %d, want 2", len(got))
	}
}

func TestSuggestionDisappearedDeviceIsNotVisible(t *testing.T) {
	s, path, _ := suggestionTestState(t)
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.Local)
	all := []Finding{suggestionFinding("gone", "dev-gone"), suggestionFinding("general", "")}
	gone := func(device string) bool { return device != "dev-gone" }

	if got := s.Visible(all, now, gone); len(got) != 1 || got[0].ID != "general" {
		t.Fatalf("visible = %+v, want only the finding without device", got)
	}

	s.SetShown(map[string]Shown{"gone": {Lower: 2, Since: 1}, "general": {Lower: 3}}, now)
	s.Prune(gone, all)
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded := LoadSuggestionState(path, nil)
	if _, ok := reloaded.Shown()["gone"]; ok {
		t.Fatalf("shown entry of the disappeared device survived Save")
	}
	if _, ok := reloaded.Shown()["general"]; !ok {
		t.Fatalf("shown entry without device was dropped")
	}
}

func TestSuggestionResetLearnedKeepsDismissed(t *testing.T) {
	s, path, _ := suggestionTestState(t)
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.Local)

	if err := s.Dismiss("a", now); err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	if err := s.Later("b", now); err != nil {
		t.Fatalf("Later: %v", err)
	}
	s.SetShown(map[string]Shown{"c": {Lower: 4}}, now)

	if err := s.ResetLearned(); err != nil {
		t.Fatalf("ResetLearned: %v", err)
	}
	if got := s.Shown(); len(got) != 0 {
		t.Fatalf("shown after ResetLearned = %v, want empty", got)
	}

	reloaded := LoadSuggestionState(path, nil)
	if got := reloaded.Visible([]Finding{suggestionFinding("a", ""), suggestionFinding("b", "")}, now, nil); len(got) != 0 {
		t.Fatalf("dismissed or postponed findings came back after ResetLearned: %d visible", len(got))
	}
	if got := reloaded.Shown(); len(got) != 0 {
		t.Fatalf("shown after reload = %v, want empty", got)
	}
}

func TestSuggestionDamagedFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "suggestions.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	var logged int
	s := LoadSuggestionState(path, func(string, ...any) { logged++ })
	if logged == 0 {
		t.Fatalf("damaged file was not logged")
	}
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.Local)
	all := []Finding{suggestionFinding("a", "")}
	if got := s.Visible(all, now, nil); len(got) != 1 {
		t.Fatalf("visible with damaged file = %d, want 1", len(got))
	}
	if err := s.Dismiss("a", now); err != nil {
		t.Fatalf("Dismiss after damaged load: %v", err)
	}
	if got := LoadSuggestionState(path, nil).Visible(all, now, nil); len(got) != 0 {
		t.Fatalf("dismissed finding visible after rewrite")
	}
}

func TestSuggestionMissingFileIsEmptyWithoutLog(t *testing.T) {
	_, _, logs := suggestionTestState(t)
	if len(*logs) != 0 {
		t.Fatalf("missing file logged: %v", *logs)
	}
}

func TestSuggestionSaveRoundTrip(t *testing.T) {
	s, path, _ := suggestionTestState(t)
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.Local)

	if err := s.Dismiss("a", now); err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	if err := s.Later("b", now); err != nil {
		t.Fatalf("Later: %v", err)
	}
	s.SetShown(map[string]Shown{
		"c": {Lower: 1.5, Values: map[string]float64{"time": 1350}},
	}, now)
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded := LoadSuggestionState(path, nil)
	all := []Finding{suggestionFinding("a", ""), suggestionFinding("b", ""), suggestionFinding("c", "")}
	if got := reloaded.Visible(all, now, nil); len(got) != 1 || got[0].ID != "c" {
		t.Fatalf("reloaded visible = %+v, want only c (a dismissed, b postponed)", got)
	}
	shown := reloaded.Shown()
	c, ok := shown["c"]
	if !ok {
		t.Fatalf("shown entry c lost on reload")
	}
	if c.Lower != 1.5 || c.Values["time"] != 1350 || c.Since != now.Unix() {
		t.Fatalf("shown c after reload = %+v", c)
	}
}

func TestSuggestionSetShownKeepsSinceOfKnownIDs(t *testing.T) {
	s, _, _ := suggestionTestState(t)
	first := time.Date(2026, 11, 20, 10, 0, 0, 0, time.Local)
	later := first.Add(time.Hour)

	s.SetShown(map[string]Shown{"a": {Lower: 1}}, first)
	s.SetShown(map[string]Shown{"a": {Lower: 2, Since: 0}, "b": {Lower: 3}}, later)

	shown := s.Shown()
	if shown["a"].Since != first.Unix() {
		t.Fatalf("Since of known id = %d, want %d", shown["a"].Since, first.Unix())
	}
	if shown["b"].Since != later.Unix() {
		t.Fatalf("Since of new id = %d, want %d", shown["b"].Since, later.Unix())
	}
}
