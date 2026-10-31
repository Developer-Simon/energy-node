package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// saveToDownloads schreibt content in das Downloads-Verzeichnis des Benutzers
// und gibt den vollen Pfad zurück.
func saveToDownloads(filename string, content []byte) (string, error) {
	return saveFileIn(downloadsDir(), filename, content)
}

// saveFileIn schreibt content unter dem bereinigten Namen in dir. Existiert die
// Datei bereits, wird ein Zähler vor der Endung ergänzt (name-1.txt, ...).
func saveFileIn(dir, filename string, content []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := safeFileName(filename)
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)

	const maxAttempts = 100
	for i := 0; i < maxAttempts; i++ {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d%s", base, i, ext)
		}
		path := filepath.Join(dir, candidate)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(content); err != nil {
			_ = f.Close()
			_ = os.Remove(path)
			return "", err
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(path)
			return "", err
		}
		return path, nil
	}
	return "", fmt.Errorf("kein freier Dateiname für %s gefunden", name)
}

// safeFileName macht aus einem Namen einen sicheren Dateinamen ohne Pfad.
func safeFileName(filename string) string {
	name := filepath.Base(filename)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.TrimLeft(b.String(), ".")
	if out == "" {
		return "energy-node.txt"
	}
	if len(out) > 100 {
		out = out[:100]
	}
	return out
}

// downloadsDir ermittelt das Downloads-Verzeichnis des Benutzers.
func downloadsDir() string {
	home, homeErr := os.UserHomeDir()

	if _, err := exec.LookPath("xdg-user-dir"); err == nil {
		out, err := exec.Command("xdg-user-dir", "DOWNLOAD").Output()
		if err == nil {
			dir := strings.TrimSpace(string(out))
			if dir != "" && dir != home {
				return dir
			}
		}
	}

	if homeErr != nil {
		return os.TempDir()
	}
	downloads := filepath.Join(home, "Downloads")
	if info, err := os.Stat(downloads); err == nil && info.IsDir() {
		return downloads
	}
	return home
}
