// Package shell ist Schicht 4: das Fenster um die Oberflaeche. Es ist die
// aeusserste, austauschbare Schicht (E6) - faellt sie aus, oeffnet sich der
// Standardbrowser und alles andere funktioniert unveraendert.
package shell

import (
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
