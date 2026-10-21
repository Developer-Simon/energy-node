package servicelogs

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
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

const testHelper = "/usr/local/sbin/h"

// fakeRunner records every call and returns canned output. If gate is set,
// Run blocks until gate is closed or the context ends. entered receives one
// value per call before the gate, so tests can wait until calls are in flight.
type fakeRunner struct {
	mu      sync.Mutex
	calls   [][]string
	stdout  []byte
	stderr  []byte
	err     error
	gate    chan struct{}
	entered chan struct{}
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, append([]string{name}, args...))
	f.mu.Unlock()
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	return f.stdout, f.stderr, f.err
}

func (f *fakeRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// exitErr stands in for *exec.ExitError.
type exitErr struct {
	code int
	msg  string
}

func (e *exitErr) Error() string { return e.msg }
func (e *exitErr) ExitCode() int { return e.code }

func fakeJournalLine(t *testing.T, cursor, message string) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{
		"__CURSOR":             cursor,
		"__REALTIME_TIMESTAMP": "1761040800123456",
		"PRIORITY":             "6",
		"_SYSTEMD_UNIT":        "mosquitto.service",
		"MESSAGE":              message,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func joinLines(lines ...string) []byte {
	return []byte(strings.Join(lines, "\n") + "\n")
}

func TestReadBuildsHelperCall(t *testing.T) {
	fake := &fakeRunner{}
	r := NewReader(fake, testHelper)
	_, err := r.Read(context.Background(), Query{Unit: "mosquitto", SinceMinutes: 15, Lines: 100, Cursor: "s=9"})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	want := []string{"sudo", "-n", testHelper, "read-journal", "mosquitto", "15", "100", "s=9"}
	if len(fake.calls) != 1 || !slices.Equal(fake.calls[0], want) {
		t.Fatalf("helper call = %q, want %q", fake.calls, want)
	}

	fake = &fakeRunner{}
	r = NewReader(fake, testHelper)
	if _, err := r.Read(context.Background(), Query{Unit: "mosquitto", SinceMinutes: 15, Lines: 100}); err != nil {
		t.Fatalf("Read() without cursor error = %v", err)
	}
	wantNoCursor := want[:len(want)-1]
	if len(fake.calls) != 1 || !slices.Equal(fake.calls[0], wantNoCursor) {
		t.Fatalf("helper call without cursor = %q, want %q", fake.calls, wantNoCursor)
	}
}

func TestReadFiltersCaseInsensitive(t *testing.T) {
	fake := &fakeRunner{stdout: joinLines(
		fakeJournalLine(t, "s=1", "MQTT connected"),
		fakeJournalLine(t, "s=2", "MQTT fehler"),
		fakeJournalLine(t, "s=3", "all good"),
	)}
	r := NewReader(fake, testHelper)
	res, err := r.Read(context.Background(), Query{Unit: "mosquitto", SinceMinutes: 60, Lines: 200, Filter: "FEHLER"})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(res.Entries) != 1 || res.Entries[0].Message != "MQTT fehler" {
		t.Fatalf("Entries = %+v, want only the fehler line", res.Entries)
	}
	if res.Cursor != "s=3" {
		t.Errorf("Cursor = %q, want s=3 (last fetched line)", res.Cursor)
	}
}

func TestReadCursorAdvancesPastFilteredEntries(t *testing.T) {
	fake := &fakeRunner{stdout: joinLines(
		fakeJournalLine(t, "s=a", "one"),
		fakeJournalLine(t, "s=b", "two"),
	)}
	r := NewReader(fake, testHelper)
	res, err := r.Read(context.Background(), Query{Unit: "dashboard", SinceMinutes: 60, Lines: 200, Filter: "nomatch"})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if res.Entries == nil || len(res.Entries) != 0 {
		t.Errorf("Entries = %#v, want empty non-nil slice", res.Entries)
	}
	if res.Cursor != "s=b" {
		t.Errorf("Cursor = %q, want s=b", res.Cursor)
	}
}

func TestReadEmptyKeepsRequestCursor(t *testing.T) {
	fake := &fakeRunner{}
	r := NewReader(fake, testHelper)
	res, err := r.Read(context.Background(), Query{Unit: "dashboard", SinceMinutes: 60, Lines: 200, Cursor: "s=x"})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if res.Cursor != "s=x" {
		t.Errorf("Cursor = %q, want s=x", res.Cursor)
	}
	if res.Entries == nil || len(res.Entries) != 0 {
		t.Errorf("Entries = %#v, want empty non-nil slice", res.Entries)
	}
	if res.Limited {
		t.Error("Limited = true for empty output")
	}
}

func TestReadLimited(t *testing.T) {
	build := func(n int) []byte {
		lines := make([]string, 0, n)
		for i := 0; i < n; i++ {
			lines = append(lines, fakeJournalLine(t, "s="+strings.Repeat("a", i+1), "msg"))
		}
		return joinLines(lines...)
	}

	r := NewReader(&fakeRunner{stdout: build(100)}, testHelper)
	res, err := r.Read(context.Background(), Query{Unit: "dashboard", SinceMinutes: 60, Lines: 100})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !res.Limited {
		t.Error("100 of 100 lines: Limited = false, want true")
	}

	r = NewReader(&fakeRunner{stdout: build(99)}, testHelper)
	res, err = r.Read(context.Background(), Query{Unit: "dashboard", SinceMinutes: 60, Lines: 100})
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if res.Limited {
		t.Error("99 of 100 lines: Limited = true, want false")
	}
}

func TestReadMapsSudoFailure(t *testing.T) {
	fake := &fakeRunner{
		stderr: []byte("sudo: a password is required\n"),
		err:    &exitErr{code: 1, msg: "exit status 1"},
	}
	r := NewReader(fake, testHelper)
	_, err := r.Read(context.Background(), Query{Unit: "dashboard", SinceMinutes: 60, Lines: 200})
	if !errors.Is(err, ErrNotPermitted) {
		t.Fatalf("Read() error = %v, want ErrNotPermitted", err)
	}
}

func TestReadMapsHelperRejection(t *testing.T) {
	fake := &fakeRunner{err: &exitErr{code: 65, msg: "exit status 65"}}
	r := NewReader(fake, testHelper)
	_, err := r.Read(context.Background(), Query{Unit: "dashboard", SinceMinutes: 60, Lines: 200})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("Read() error = %v, want ErrInvalidQuery", err)
	}
}

func TestReadMapsJournalFailure(t *testing.T) {
	fake := &fakeRunner{
		stderr: []byte("Failed to open journal\n"),
		err:    &exitErr{code: 1, msg: "exit status 1"},
	}
	r := NewReader(fake, testHelper)
	_, err := r.Read(context.Background(), Query{Unit: "dashboard", SinceMinutes: 60, Lines: 200})
	if !errors.Is(err, ErrJournal) {
		t.Fatalf("Read() error = %v, want ErrJournal", err)
	}
	if !strings.Contains(err.Error(), "Failed to open journal") {
		t.Errorf("error text %q lacks the journal detail", err.Error())
	}
}

func TestReadTimeout(t *testing.T) {
	fake := &fakeRunner{gate: make(chan struct{})}
	r := NewReader(fake, testHelper)
	r.timeout = 20 * time.Millisecond
	_, err := r.Read(context.Background(), Query{Unit: "dashboard", SinceMinutes: 60, Lines: 200})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Read() error = %v, want ErrTimeout", err)
	}
}

func TestReadBusy(t *testing.T) {
	gate := make(chan struct{})
	fake := &fakeRunner{gate: gate, entered: make(chan struct{}, MaxConcurrent)}
	r := NewReader(fake, testHelper)
	q := Query{Unit: "dashboard", SinceMinutes: 60, Lines: 200}

	var wg sync.WaitGroup
	for i := 0; i < MaxConcurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = r.Read(context.Background(), q)
		}()
	}
	for i := 0; i < MaxConcurrent; i++ {
		<-fake.entered
	}

	if _, err := r.Read(context.Background(), q); !errors.Is(err, ErrBusy) {
		t.Errorf("third Read() error = %v, want ErrBusy", err)
	}

	close(gate)
	wg.Wait()
}

func TestReadRejectsInvalidQueryWithoutRunning(t *testing.T) {
	fake := &fakeRunner{}
	r := NewReader(fake, testHelper)
	_, err := r.Read(context.Background(), Query{Unit: "sshd", SinceMinutes: 60, Lines: 200})
	if !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("Read() error = %v, want ErrInvalidQuery", err)
	}
	if n := fake.callCount(); n != 0 {
		t.Errorf("runner called %d times, want 0", n)
	}
}
