package unitlogs

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
)

// maxLineBytes begrenzt eine Journal-Zeile beim Parsen.
const maxLineBytes = 1 << 20

// maxDetailRunes begrenzt den stderr-Text, der mit UNIT_LOGS_FAILED kommt.
const maxDetailRunes = 500

// Runner fuehrt Befehle auf dem Wirt aus. *transport.Client erfuellt es.
type Runner interface {
	Run(ctx context.Context, command string, stdout, stderr io.Writer) error
	RunWithStdin(ctx context.Context, command string, stdin io.Reader, stdout, stderr io.Writer) error
}

// Stream meldet Hostname, Unit-Status und Journal-Zeilen an sink, bis ctx
// endet oder journalctl vorher stirbt. Status kommt nur bei Aenderung, Zeilen
// durchlaufen redact.
func Stream(ctx context.Context, r Runner, req hostapi.UnitLogsRequest, redact *hostapi.Redactor, sink hostapi.UnitLogSink, poll time.Duration) error {
	var hostname bytes.Buffer
	if err := r.Run(ctx, "hostname", &hostname, io.Discard); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	sink.Hello(strings.TrimSpace(hostname.String()))

	pollCtx, stopPolling := context.WithCancel(ctx)
	var pollers sync.WaitGroup
	for _, unit := range req.Units {
		pollers.Add(1)
		go func() {
			defer pollers.Done()
			pollStatus(pollCtx, r, unit, sink, poll)
		}()
	}

	err := journal(ctx, r, req, redact, sink)
	stopPolling()
	pollers.Wait()
	return err
}

// pollStatus fragt den Zustand einer Unit sofort und danach alle poll ab und
// meldet nur Aenderungen. Fehlschlaege der Abfrage werden uebersprungen.
func pollStatus(ctx context.Context, r Runner, unit string, sink hostapi.UnitLogSink, poll time.Duration) {
	var last hostapi.UnitStatus
	sent := false
	check := func() {
		var out bytes.Buffer
		if err := r.Run(ctx, StatusCommand(unit), &out, io.Discard); err != nil {
			return
		}
		st := ParseStatus(unit, out.String())
		if sent && st == last {
			return
		}
		last, sent = st, true
		sink.Status(st)
	}

	check()
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}

// journal laeuft als Remote-Skript, bis ctx endet. Der Schreibende des
// Stdin-Pipes wird beim Ende geschlossen, so beendet der Waechter journalctl.
func journal(ctx context.Context, r Runner, req hostapi.UnitLogsRequest, redact *hostapi.Redactor, sink hostapi.UnitLogSink) error {
	stdinR, stdinW := io.Pipe()
	stopWatch := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-stopWatch:
		}
		_ = stdinW.Close()
	}()

	stdoutR, stdoutW := io.Pipe()
	parsed := make(chan struct{})
	go func() {
		defer close(parsed)
		scanner := bufio.NewScanner(stdoutR)
		scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
		for scanner.Scan() {
			line, ok := ParseJournalLine(scanner.Bytes(), req.Units)
			if !ok {
				continue
			}
			if redact != nil {
				line.Message = redact.Line(line.Message)
			}
			sink.Line(line)
		}
		_, _ = io.Copy(io.Discard, stdoutR)
	}()

	var stderr bytes.Buffer
	command := "sh -c " + transport.ShellQuote(JournalCommand(req))
	_ = r.RunWithStdin(ctx, command, stdinR, stdoutW, &stderr)

	_ = stdoutW.Close()
	<-parsed
	close(stopWatch)
	_ = stdinW.Close()
	_ = stdinR.Close()

	if ctx.Err() != nil {
		return ctx.Err()
	}
	return &hostapi.Error{Code: "UNIT_LOGS_FAILED", Detail: truncateDetail(strings.TrimSpace(stderr.String()))}
}

// truncateDetail kuerzt auf maxDetailRunes Zeichen.
func truncateDetail(s string) string {
	if utf8.RuneCountInString(s) <= maxDetailRunes {
		return s
	}
	return string([]rune(s)[:maxDetailRunes])
}
