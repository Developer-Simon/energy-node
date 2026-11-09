package backup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/Developer-Simon/energy-node-dashboard/internal/config"
)

const settingsFile = "backup.json"

// Settings beschreibt, wann ein Backup entsteht und was hineinkommt.
type Settings struct {
	BeforeUpdate   bool          `json:"before_update"`
	Schedule       bool          `json:"schedule"`
	Every          string        `json:"every"` // "day" oder "week"
	Keep           int           `json:"keep"`
	SystemFallback bool          `json:"system_fallback"`
	Secrets        bool          `json:"secrets"`
	Content        map[Area]bool `json:"content"`
}

// DefaultSettings liefert die Einstellungen, die gelten, solange nichts gesetzt ist.
func DefaultSettings() Settings {
	return Settings{
		BeforeUpdate:   true,
		Schedule:       false,
		Every:          "week",
		Keep:           5,
		SystemFallback: true,
		Secrets:        false,
		Content: map[Area]bool{
			AreaSettings: true,
			AreaDevices:  true,
			AreaUsers:    true,
			AreaStats:    true,
			AreaHistory:  true,
			AreaSecrets:  false,
			AreaBattery:  false,
		},
	}
}

// LoadSettings liest data_dir/backup.json. Fehlt die Datei, gelten die Defaults.
// Fehlende Felder behalten ihren Default, ungueltige Werte ebenso.
func LoadSettings(dataDir string) (Settings, error) {
	s := DefaultSettings()
	raw, err := os.ReadFile(filepath.Join(dataDir, settingsFile))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return Settings{}, err
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return Settings{}, err
	}
	return sanitizeSettings(s), nil
}

// SaveSettings schreibt die Einstellungen atomar mit Modus 0600.
func SaveSettings(dataDir string, s Settings) error {
	raw, err := json.MarshalIndent(sanitizeSettings(s), "", "  ")
	if err != nil {
		return err
	}
	return config.AtomicWrite(filepath.Join(dataDir, settingsFile), append(raw, '\n'), 0o600)
}

// sanitizeSettings ersetzt ungueltige Felder durch ihren Default.
func sanitizeSettings(s Settings) Settings {
	d := DefaultSettings()
	if s.Every != "day" && s.Every != "week" {
		s.Every = d.Every
	}
	if s.Keep < 1 {
		s.Keep = d.Keep
	}
	if s.Content == nil {
		s.Content = d.Content
	}
	return s
}

// ErrNoLocation meldet, dass kein Ort fuer Archive verfuegbar ist.
var ErrNoLocation = errors.New("backup: no location")

// Location liefert das Verzeichnis, in dem Archive liegen, und dessen Namen.
type Location interface {
	ArchiveDir() (dir string, place string, err error)
}

// SystemLocation legt Archive im Systemlaufwerk unter DataDir/backups ab.
// Plan 2 ergaenzt ein eigenes Laufwerk.
type SystemLocation struct {
	DataDir  string
	Settings func() Settings
}

// ArchiveDir liefert DataDir/backups und den Ort "system", sofern das
// Systemlaufwerk erlaubt ist. Sonst ErrNoLocation.
func (l SystemLocation) ArchiveDir() (string, string, error) {
	s := DefaultSettings()
	if l.Settings != nil {
		s = l.Settings()
	}
	if !s.SystemFallback {
		return "", "", ErrNoLocation
	}
	dir := filepath.Join(l.DataDir, "backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	return dir, "system", nil
}
