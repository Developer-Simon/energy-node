// Package shell ist Schicht 4: das Fenster um die Oberflaeche. Es ist die
// aeusserste, austauschbare Schicht (E6) - faellt sie aus, oeffnet sich der
// Standardbrowser und alles andere funktioniert unveraendert.
package shell

import (
	"fmt"
	"os/exec"
	"runtime"
)

// Candidate ist ein Programm, das ein rahmenloses Fenster oeffnen kann.
type Candidate struct {
	Name string
	Args func(url string) []string
}

// Candidates liefert die Startversuche in der Reihenfolge, in der sie
// unternommen werden.
func Candidates(goos string) []Candidate {
	appArgs := func(url string) []string { return []string{"--app=" + url} }
	switch goos {
	case "windows":
		return []Candidate{
			{Name: "msedge", Args: appArgs},
			{Name: "chrome", Args: appArgs},
			{Name: "chromium", Args: appArgs},
		}
	case "darwin":
		mac := func(app string) Candidate {
			return Candidate{Name: "open", Args: func(url string) []string {
				return []string{"-a", app, "--args", "--app=" + url}
			}}
		}
		return []Candidate{mac("Google Chrome"), mac("Chromium"), mac("Microsoft Edge")}
	default:
		return []Candidate{
			{Name: "google-chrome", Args: appArgs},
			{Name: "chromium", Args: appArgs},
			{Name: "chromium-browser", Args: appArgs},
			{Name: "microsoft-edge", Args: appArgs},
		}
	}
}

// Open oeffnet die URL. mode ist "app", wenn ein rahmenloses Fenster
// aufging, und "browser", wenn der Standardbrowser einsprang.
func Open(url string) (string, error) {
	for _, candidate := range Candidates(runtime.GOOS) {
		path, err := exec.LookPath(candidate.Name)
		if err != nil {
			continue
		}
		cmd := exec.Command(path, candidate.Args(url)...)
		if err := cmd.Start(); err != nil {
			continue
		}
		// Nicht auf das Fenster warten: es lebt laenger als dieser Aufruf, und
		// der Server muss weiterlaufen. Das Kind wird beim Beenden des
		// Installers ohnehin vom Betriebssystem uebernommen.
		go func() { _ = cmd.Wait() }()
		return "app", nil
	}
	if err := openInBrowser(url); err != nil {
		return "", fmt.Errorf("shell: kein Fenster und kein Browser: %w", err)
	}
	return "browser", nil
}

func openInBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
