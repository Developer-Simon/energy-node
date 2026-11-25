package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// HelperName ist der Unterbefehl, der RunHelper im Installer-Binary startet.
const HelperName = "media-helper"

// ShimName ist der Unterbefehl, der unter Windows den Imager in der Pseudokonsole startet.
const ShimName = "media-shim"

// HelperFiles sind die Dateien, ueber die Installer und Helfer sprechen. Status ist optional.
type HelperFiles struct{ Log, Cancel, Status string }

// imagerProcess ist der laufende Imager, plattformspezifisch erzeugt.
type imagerProcess interface {
	Wait() (int, error)
	Kill() error
}

// PrepareHelperFiles legt Log und Status leer an, als aufrufender Benutzer.
// Die Abbruchdatei wird nicht angelegt.
func PrepareHelperFiles(dir string) (HelperFiles, error) {
	f := HelperFiles{
		Log:    filepath.Join(dir, "imager.log"),
		Cancel: filepath.Join(dir, "cancel"),
		Status: filepath.Join(dir, "status"),
	}
	for _, p := range []string{f.Log, f.Status} {
		file, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return HelperFiles{}, err
		}
		if err := file.Close(); err != nil {
			return HelperFiles{}, err
		}
	}
	return f, nil
}

// ReadHelperStatus liest den Exit-Code aus der Statusdatei. false bei leer oder kaputt.
func ReadHelperStatus(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	code, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}
	return code, true
}

// checkImagerName erlaubt nur den Imager selbst, nicht beliebige Programme.
func checkImagerName(name string) error {
	base := filepath.Base(name)
	if base != "rpi-imager.exe" && base != "rpi-imager" {
		return fmt.Errorf("refusing to run %s", base)
	}
	return nil
}

// RunHelper startet den Imager, schreibt seine Ausgabe ins Log und liefert den Exit-Code.
// Erscheint die Abbruchdatei, wird der Imager beendet.
func RunHelper(ctx context.Context, f HelperFiles, name string, args []string) int {
	log, err := os.OpenFile(f.Log, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return finishHelper(f, 125)
	}
	defer log.Close()

	if err := checkImagerName(name); err != nil {
		fmt.Fprintf(log, "Error: %v\n", err)
		return finishHelper(f, 125)
	}
	if fileExists(f.Cancel) {
		fmt.Fprintln(log, "Error: cancelled before start")
		return finishHelper(f, 1)
	}

	proc, err := startImager(log, name, args)
	if err != nil {
		fmt.Fprintf(log, "Error: %v\n", err)
		return finishHelper(f, 125)
	}

	type waitResult struct {
		code int
		err  error
	}
	waited := make(chan waitResult, 1)
	go func() {
		code, err := proc.Wait()
		waited <- waitResult{code, err}
	}()

	killed := false
	kill := func() {
		if killed {
			return
		}
		killed = true
		if err := proc.Kill(); err != nil {
			fmt.Fprintf(log, "Error: kill: %v\n", err)
		}
	}

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	cancelled := ctx.Done()
	for {
		select {
		case r := <-waited:
			if r.err != nil {
				fmt.Fprintf(log, "Error: %v\n", r.err)
				return finishHelper(f, 125)
			}
			code := r.code
			if code < 0 {
				// Durch Signal beendet: nie als Erfolg oder ungueltiger Code melden.
				code = 1
			}
			return finishHelper(f, code)
		case <-ticker.C:
			if fileExists(f.Cancel) {
				kill()
			}
		case <-cancelled:
			cancelled = nil
			kill()
		}
	}
}

// finishHelper schreibt den Exit-Code in die Statusdatei, falls eine gesetzt ist.
func finishHelper(f HelperFiles, code int) int {
	if f.Status != "" {
		if file, err := os.OpenFile(f.Status, os.O_WRONLY|os.O_TRUNC, 0); err == nil {
			_, _ = fmt.Fprintf(file, "%d\n", code)
			_ = file.Close()
		}
	}
	return code
}

// RunShim startet name mit den geerbten Standardkanaelen und liefert dessen Exit-Code.
// Die Kindprozesse erben bewusst die Konsole des Shims.
func RunShim(name string, args []string) int {
	if err := checkImagerName(name); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 125
	}
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if code := ee.ExitCode(); code >= 0 {
			return code
		}
		return 1
	}
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	return 125
}

// startExec startet name mit Ausgabe ins Log, ohne Pseudokonsole.
// Die Plattformdateien rufen es aus startImager auf.
func startExec(log *os.File, name string, args []string) (imagerProcess, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = log
	cmd.Stderr = log
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return execImager{cmd}, nil
}

type execImager struct{ cmd *exec.Cmd }

func (e execImager) Wait() (int, error) {
	err := e.cmd.Wait()
	if err == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return 0, err
}

func (e execImager) Kill() error { return e.cmd.Process.Kill() }

// TailLog liefert alle Bytes, die in path geschrieben werden. Die Datei darf anfangs fehlen.
// Ist done geschlossen, liest es den Rest und liefert danach io.EOF.
// Close beendet das Folgen sofort und gibt die Datei frei.
func TailLog(path string, done <-chan struct{}) io.ReadCloser {
	return &tailReader{
		path: path,
		done: done,
		stop: make(chan struct{}),
	}
}

const tailPoll = 100 * time.Millisecond

type tailReader struct {
	path string
	done <-chan struct{}
	stop chan struct{}
	once sync.Once

	mu sync.Mutex
	f  *os.File
}

func (t *tailReader) Read(p []byte) (int, error) {
	for {
		if isClosed(t.stop) {
			return 0, io.EOF
		}
		// Erst pruefen, dann lesen: so geht kein Byte verloren, das vor done geschrieben wurde.
		finished := isClosed(t.done)
		n, err := t.readOnce(p)
		if n > 0 {
			return n, nil
		}
		if err != nil {
			return 0, err
		}
		if finished {
			_ = t.Close()
			return 0, io.EOF
		}
		select {
		case <-t.done:
		case <-t.stop:
		case <-time.After(tailPoll):
		}
	}
}

func (t *tailReader) readOnce(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if isClosed(t.stop) {
		return 0, io.EOF
	}
	if t.f == nil {
		f, err := os.Open(t.path)
		if err != nil {
			return 0, nil // Datei fehlt noch
		}
		t.f = f
	}
	n, err := t.f.Read(p)
	if errors.Is(err, io.EOF) {
		// Noch kein neues Ende: der Aufrufer wartet auf weitere Bytes.
		return n, nil
	}
	return n, err
}

func (t *tailReader) Close() error {
	t.once.Do(func() { close(t.stop) })
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.f != nil {
		err := t.f.Close()
		t.f = nil
		return err
	}
	return nil
}

func isClosed(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
