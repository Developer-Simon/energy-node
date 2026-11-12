package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Developer-Simon/energy-node-dashboard/internal/storage"
)

// storageDemoEnv nennt ein Verzeichnis für Demo und Smoke-Test. Ist es gesetzt,
// liest die Laufwerksliste drives.json darin statt lsblk, der Einhängepunkt
// liegt unter mount/ und der RAM-Ausweich unter ram/. Die Node sieht dann keine
// echten Datenträger. Der Speicherhelfer ist in diesem Lauf der Fake der Demo.
const storageDemoEnv = "ENERGY_NODE_STORAGE_DEMO"

// storagePath wählt den Demo-Ort unter dem Demo-Verzeichnis, sonst den echten Pfad.
func storagePath(demoDir, name, real string) string {
	if demoDir == "" {
		return real
	}
	return filepath.Join(demoDir, name)
}

// storageDriveLister liefert die Laufwerksliste: die Demo-Datei, sonst lsblk.
func storageDriveLister() storage.Lister {
	if dir := os.Getenv(storageDemoEnv); dir != "" {
		return demoDriveLister{Path: filepath.Join(dir, "drives.json")}
	}
	return storage.LsblkLister{}
}

// demoDriveLister liest die Laufwerke aus einer JSON-Liste im selben Format wie
// lsblk-Auswertung (storage.Drive). Fehlt die Datei, gibt es keine Laufwerke.
type demoDriveLister struct {
	Path string
}

// Drives liest die Datei bei jedem Aufruf neu, damit ein eingesteckter Demo-Stick
// sofort erscheint, wenn die Datei geändert wird.
func (l demoDriveLister) Drives(_ context.Context) ([]storage.Drive, error) {
	raw, err := os.ReadFile(l.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return []storage.Drive{}, nil
	}
	if err != nil {
		return nil, err
	}
	var drives []storage.Drive
	if err := json.Unmarshal(raw, &drives); err != nil {
		return nil, err
	}
	if drives == nil {
		drives = []storage.Drive{}
	}
	return drives, nil
}
