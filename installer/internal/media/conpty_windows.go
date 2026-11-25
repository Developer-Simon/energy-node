package media

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Obergrenzen: alle Prozesse des Jobs muessen in dieser Zeit enden, die Konsolenausgabe in der zweiten.
const (
	jobWaitLimit  = 60 * time.Second
	copyWaitLimit = 5 * time.Second
)

// conptyImager ist der laufende Imager in der Pseudokonsole. Kill und Wait laufen in verschiedenen
// Goroutinen. mu schuetzt killed, closed und members sowie die Handles, die Kill liest.
type conptyImager struct {
	log *os.File

	mu       sync.Mutex
	killed   bool
	closed   bool
	members  []windows.Handle
	job      windows.Handle
	proc     windows.Handle
	thread   windows.Handle
	hpc      windows.Handle
	inR, inW windows.Handle
	outR     windows.Handle
	outW     windows.Handle
	out      *os.File
	copyDone chan struct{}
}

// Kill beendet alle Prozesse des Jobs. Prozesse, die sich nicht mehr oeffnen lassen, verhindern das nicht.
func (w *conptyImager) Kill() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.killed {
		return nil
	}
	w.killed = true
	// Handles vor dem Beenden oeffnen: ein Handle ist erst signalisiert, wenn der Prozess weg ist.
	members, err := openJobProcesses(w.job, w.log)
	if err != nil {
		fmt.Fprintln(w.log, "list job processes:", err)
	}
	w.members = members
	if err := windows.TerminateJobObject(w.job, 1); err != nil {
		return fmt.Errorf("TerminateJobObject: %w", err)
	}
	return nil
}

// Wait wartet auf den Zwischenprozess, der den Imager-Exit-Code weitergibt, und raeumt alles auf.
func (w *conptyImager) Wait() (int, error) {
	// Ohne Sperre warten, sonst blockiert Kill waehrend des Wartens.
	var resErr error
	rc, err := windows.WaitForSingleObject(w.proc, windows.INFINITE)
	if err != nil {
		resErr = fmt.Errorf("WaitForSingleObject: %w", err)
	} else if rc != windows.WAIT_OBJECT_0 {
		resErr = fmt.Errorf("WaitForSingleObject returned %#x", rc)
	}
	var exit uint32
	if err := windows.GetExitCodeProcess(w.proc, &exit); err != nil && resErr == nil {
		resErr = fmt.Errorf("GetExitCodeProcess: %w", err)
	}

	w.mu.Lock()
	w.closed = true
	members := w.members
	w.members = nil
	w.mu.Unlock()

	deadline := time.Now().Add(jobWaitLimit)
	for _, m := range members {
		left := max(time.Until(deadline), 0)
		rc, err := windows.WaitForSingleObject(m, uint32(left/time.Millisecond))
		if (err != nil || rc != windows.WAIT_OBJECT_0) && resErr == nil {
			resErr = fmt.Errorf("process still active: rc=%#x err=%v", rc, err)
		}
		_ = windows.CloseHandle(m)
	}

	w.closePseudoConsole()
	select {
	case <-w.copyDone:
	case <-time.After(copyWaitLimit):
	}
	w.closeAll()

	// Der Imager-Exit-Code kommt als uint32, Abstuerze wie 0xC0000005 werden so negativ.
	return int(int32(exit)), resErr
}

func (w *conptyImager) closePseudoConsole() {
	if w.hpc != 0 {
		windows.ClosePseudoConsole(w.hpc)
		w.hpc = 0
	}
}

// closeAll gibt alle noch offenen Handles frei. Doppeltes Schliessen ist sicher.
func (w *conptyImager) closeAll() {
	w.closePseudoConsole()
	closeHandle(&w.inR)
	closeHandle(&w.outW)
	closeHandle(&w.inW)
	closeHandle(&w.outR)
	if w.out != nil {
		_ = w.out.Close()
		w.out = nil
	}
	closeHandle(&w.thread)
	closeHandle(&w.proc)
	closeHandle(&w.job)
}

func closeHandle(h *windows.Handle) {
	if *h != 0 {
		_ = windows.CloseHandle(*h)
		*h = 0
	}
}

// newKillOnCloseJob legt einen Job an, dessen Prozesse enden, sobald das letzte Handle schliesst.
func newKillOnCloseJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("CreateJobObject: %w", err)
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return 0, fmt.Errorf("SetInformationJobObject: %w", err)
	}
	return job, nil
}

// openJobProcesses liefert ein SYNCHRONIZE-Handle fuer jeden Prozess im Job und protokolliert jeden.
// Auch bei einem Fehler kommen die Handles zurueck, die sich oeffnen liessen.
func openJobProcesses(job windows.Handle, log io.Writer) ([]windows.Handle, error) {
	// JOBOBJECT_BASIC_PROCESS_ID_LIST, Informationsklasse 3.
	var list struct {
		Assigned, InList uint32
		IDs              [64]uintptr
	}
	if err := windows.QueryInformationJobObject(job, 3,
		uintptr(unsafe.Pointer(&list)), uint32(unsafe.Sizeof(list)), nil); err != nil {
		return nil, fmt.Errorf("QueryInformationJobObject: %w", err)
	}
	var handles []windows.Handle
	var firstErr error
	for _, id := range list.IDs[:min(int(list.InList), len(list.IDs))] {
		h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(id))
		if err != nil {
			fmt.Fprintf(log, "job process pid=%d: open error: %v\n", id, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		name := make([]uint16, 1024)
		n := uint32(len(name))
		if err := windows.QueryFullProcessImageName(h, 0, &name[0], &n); err != nil {
			fmt.Fprintf(log, "job process pid=%d: name error: %v\n", id, err)
		} else {
			fmt.Fprintf(log, "job process pid=%d %s\n", id, windows.UTF16ToString(name[:n]))
		}
		handles = append(handles, h)
	}
	return handles, firstErr
}
