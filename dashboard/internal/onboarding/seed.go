package onboarding

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/nodeagent"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

// SeedFileName ist die Datei mit den Vorgaben des Installer-Dashboard-Schritts.
// Sie liegt neben auth.pw und wird beim Start einmal übernommen.
const SeedFileName = "onboarding-seed.json"

// seedVersion ist das einzige Format, das ApplySeed kennt.
const seedVersion = 1

// seedActor steht im Log, wenn ApplySeed Konten anlegt.
const seedActor = "onboarding-seed"

// seedFile entspricht dem Schema aus dem Installer (dashboardPrefs plus
// version). Ein Zeiger ist nil, wenn das Feld fehlt oder null ist, und dann
// bleibt die Einstellung unverändert.
type seedFile struct {
	Version           int        `json:"version"`
	AdminUser         string     `json:"admin_user"`
	Users             []seedUser `json:"users"`
	GuestAccess       *bool      `json:"guest_access"`
	Theme             *string    `json:"theme"`
	Title             *string    `json:"title"`
	Subtitle          *string    `json:"subtitle"`
	StatusBarItems    *[]string  `json:"status_bar_items"`
	ShowRuntimeStatus *bool      `json:"show_runtime_status"`
	WidePanels        *[]string  `json:"wide_panels"`
	UpdateCheck       *bool      `json:"update_check"`
	Metrics           *[]string  `json:"metrics"`
	Later             []string   `json:"later"`
}

type seedUser struct {
	Name     string `json:"name"`
	Template string `json:"template"`
	Password string `json:"password"`
}

// seedGroups sind die Gruppen des Installer-Schritts. Ihre IDs decken sich mit
// den Schritt-IDs der Einrichtungsführung.
var seedGroups = []string{"users", "guest", "theme", "title", "statusbar", "tabwidth", "updates", "metrics"}

// ApplySeed übernimmt die Vorgaben aus path einmal beim Start und löscht die
// Datei danach, auch wenn Teile scheitern, denn sie enthält Passwörter. Eine
// fehlende Datei ist kein Fehler. Fehler werden gesammelt zurückgegeben, damit
// der Start weiterläuft. Passwörter landen weder im Log noch in Fehlern.
func ApplySeed(path string, mgr *auth.Manager, st *settings.Store, ob *Store, log *slog.Logger) (err error) {
	data, readErr := os.ReadFile(path)
	if errors.Is(readErr, os.ErrNotExist) {
		return nil
	}
	defer func() {
		if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("%s nicht gelöscht: %w", SeedFileName, rmErr))
		}
	}()
	if readErr != nil {
		return fmt.Errorf("%s nicht lesbar: %w", SeedFileName, readErr)
	}

	var seed seedFile
	if jsonErr := json.Unmarshal(data, &seed); jsonErr != nil {
		log.Warn("onboarding-seed: kein gültiges JSON, Vorgaben werden ignoriert")
		return nil
	}
	if seed.Version != seedVersion {
		log.Warn("onboarding-seed: unbekannte Version, Vorgaben werden ignoriert", "version", seed.Version)
		return nil
	}
	if seed.AdminUser != "" {
		log.Warn("onboarding-seed: admin_user wird nicht übernommen, das Konto aus auth.pw lässt sich nicht umbenennen")
	}

	var errs []error
	errs = append(errs, applySeedUsers(seed.Users, mgr, log)...)
	if seed.GuestAccess != nil {
		if err := mgr.SetGuestAccess(seedActor, *seed.GuestAccess); err != nil {
			errs = append(errs, fmt.Errorf("gastzugang: %w", err))
		}
	}
	if err := applySeedSettings(seed, st); err != nil {
		errs = append(errs, err)
	}
	if err := applySeedMetrics(seed, st); err != nil {
		errs = append(errs, err)
	}
	errs = append(errs, applySeedSteps(seed, ob, log)...)
	return errors.Join(errs...)
}

// applySeedUsers legt je Eintrag ein Konto an. Ein fehlgeschlagenes Konto
// hält die übrigen nicht auf.
func applySeedUsers(users []seedUser, mgr *auth.Manager, log *slog.Logger) []error {
	var errs []error
	for _, user := range users {
		roles, err := templateRoles(user.Template)
		if err != nil {
			errs = append(errs, fmt.Errorf("konto %s: %w", user.Name, err))
			continue
		}
		if _, err := mgr.CreateUser(seedActor, user.Name, user.Password, roles); err != nil {
			errs = append(errs, fmt.Errorf("konto %s: %w", user.Name, err))
			continue
		}
		log.Info("onboarding-seed: konto angelegt", "name", user.Name, "vorlage", user.Template)
	}
	return errs
}

// templateRoles übersetzt die Vorlage eines Nutzers in Berechtigungen.
func templateRoles(template string) ([]string, error) {
	switch template {
	case "view":
		return auth.ViewRoles(), nil
	case "operate":
		return append(auth.ViewRoles(), auth.RoleOperateDevices, auth.RoleOperateAutomations), nil
	case "admin":
		return auth.AdminRoles(), nil
	}
	return nil, fmt.Errorf("unbekannte Vorlage %q", template)
}

// applySeedSettings setzt die Einstellungen, die die Vorgaben nennen. Gespeichert
// wird nur, wenn mindestens eine davon gesetzt ist.
func applySeedSettings(seed seedFile, st *settings.Store) error {
	if seed.Theme == nil && seed.Title == nil && seed.Subtitle == nil && seed.StatusBarItems == nil &&
		seed.ShowRuntimeStatus == nil && seed.WidePanels == nil && seed.UpdateCheck == nil {
		return nil
	}
	set, err := st.LoadSettings()
	if err != nil {
		return fmt.Errorf("einstellungen laden: %w", err)
	}
	if seed.Theme != nil {
		set.Theme = *seed.Theme
	}
	if seed.Title != nil {
		set.DashboardTitle = *seed.Title
	}
	if seed.Subtitle != nil {
		set.DashboardSubtitle = *seed.Subtitle
	}
	if seed.StatusBarItems != nil {
		set.StatusBarItems = slices.Clone(*seed.StatusBarItems)
	}
	if seed.ShowRuntimeStatus != nil {
		set.ShowRuntimeStatus = *seed.ShowRuntimeStatus
	}
	if seed.WidePanels != nil {
		set.WidePanels = slices.Clone(*seed.WidePanels)
	}
	if seed.UpdateCheck != nil {
		set.UpdateCheckDisabled = !*seed.UpdateCheck
	}
	if err := st.SaveSettings(set); err != nil {
		return fmt.Errorf("einstellungen speichern: %w", err)
	}
	return nil
}

// applySeedMetrics schaltet die Systemmetriken ein, die die Liste nennt, und
// schaltet die übrigen ab.
func applySeedMetrics(seed seedFile, st *settings.Store) error {
	if seed.Metrics == nil {
		return nil
	}
	cfg, err := st.LoadMQTT()
	if err != nil {
		return fmt.Errorf("mqtt laden: %w", err)
	}
	metrics := make(map[string]bool, len(nodeagent.Metrics))
	for _, metric := range nodeagent.Metrics {
		metrics[metric] = slices.Contains(*seed.Metrics, metric)
	}
	cfg.Metrics = metrics
	if err := st.SaveMQTT(cfg); err != nil {
		return fmt.Errorf("mqtt speichern: %w", err)
	}
	return nil
}

// applySeedSteps markiert die Schritte: später konfigurieren hat Vorrang vor
// den gesetzten Vorgaben.
func applySeedSteps(seed seedFile, ob *Store, log *slog.Logger) []error {
	var errs []error
	for _, id := range seed.Later {
		if !slices.Contains(seedGroups, id) {
			log.Warn("onboarding-seed: unbekannter Schritt in later, wird ignoriert", "id", id)
			continue
		}
		if err := ob.SetStep(id, StepLater); err != nil {
			errs = append(errs, fmt.Errorf("schritt %s: %w", id, err))
		}
	}
	for _, id := range seedGroups {
		if !seedSets(seed, id) || slices.Contains(seed.Later, id) {
			continue
		}
		if err := ob.SetStep(id, StepInstaller); err != nil {
			errs = append(errs, fmt.Errorf("schritt %s: %w", id, err))
		}
	}
	return errs
}

// seedSets meldet, ob die Vorgaben eine Gruppe setzen. Leere Werte zählen
// nicht, so wie der Installer sie als unverändert behandelt.
func seedSets(seed seedFile, id string) bool {
	switch id {
	case "users":
		return len(seed.Users) > 0
	case "guest":
		return seed.GuestAccess != nil
	case "theme":
		return seed.Theme != nil
	case "title":
		return nonEmpty(seed.Title) || nonEmpty(seed.Subtitle)
	case "statusbar":
		return seed.StatusBarItems != nil || seed.ShowRuntimeStatus != nil
	case "tabwidth":
		return seed.WidePanels != nil
	case "updates":
		return seed.UpdateCheck != nil
	case "metrics":
		return seed.Metrics != nil
	}
	return false
}

func nonEmpty(value *string) bool {
	return value != nil && *value != ""
}
