package onboarding

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Developer-Simon/energy-node-dashboard/internal/auth"
	"github.com/Developer-Simon/energy-node-dashboard/internal/settings"
)

const seedPassword = "geheim-passwort-42"

type seedFixture struct {
	dir      string
	path     string
	mgr      *auth.Manager
	settings *settings.Store
	ob       *Store
	logs     *bytes.Buffer
	log      *slog.Logger
}

func newSeedFixture(t *testing.T) seedFixture {
	t.Helper()
	dir := t.TempDir()
	mgr, err := auth.NewManager(filepath.Join(dir, "users.json"), "admin", "admin-passwort-1")
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	return seedFixture{
		dir:      dir,
		path:     filepath.Join(dir, "onboarding-seed.json"),
		mgr:      mgr,
		settings: settings.NewStore(dir),
		ob:       NewStore(dir),
		logs:     logs,
		log:      slog.New(slog.NewTextHandler(logs, nil)),
	}
}

func (f seedFixture) write(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile(f.path, []byte(content), 0640); err != nil {
		t.Fatal(err)
	}
}

func (f seedFixture) apply(t *testing.T) error {
	t.Helper()
	return ApplySeed(f.path, f.mgr, f.settings, f.ob, f.log)
}

func (f seedFixture) userNames() []string {
	var names []string
	for _, user := range f.mgr.Users() {
		names = append(names, user.Username)
	}
	return names
}

func (f seedFixture) fileGone(t *testing.T) bool {
	t.Helper()
	_, err := os.Stat(f.path)
	return os.IsNotExist(err)
}

func TestSeedMissingFileIsSilent(t *testing.T) {
	f := newSeedFixture(t)
	if err := f.apply(t); err != nil {
		t.Fatalf("missing seed file returned %v, want nil", err)
	}
	if f.logs.Len() != 0 {
		t.Fatalf("missing seed file logged %q", f.logs.String())
	}
}

func TestSeedAppliesOnce(t *testing.T) {
	f := newSeedFixture(t)
	f.write(t, `{"version":1,"theme":"stromblau","users":[{"name":"anna","template":"view","password":"`+seedPassword+`"}]}`)
	if err := f.apply(t); err != nil {
		t.Fatal(err)
	}
	// Der Nutzer ändert danach etwas. Ein zweiter Aufruf ohne Datei darf nichts zurückdrehen.
	set, err := f.settings.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	set.Theme = settings.ThemeMint
	if err := f.settings.SaveSettings(set); err != nil {
		t.Fatal(err)
	}
	usersBefore := f.userNames()

	if err := f.apply(t); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	after, _ := f.settings.LoadSettings()
	if after.Theme != settings.ThemeMint {
		t.Fatalf("second apply changed theme to %q", after.Theme)
	}
	if !slices.Equal(f.userNames(), usersBefore) {
		t.Fatalf("second apply changed users: %v", f.userNames())
	}
}

func TestSeedDeletesFileEvenOnPartialFailure(t *testing.T) {
	f := newSeedFixture(t)
	// Das Kennwort ist zu kurz, der Nutzer scheitert. Das Theme muss trotzdem gesetzt werden.
	f.write(t, `{"version":1,"theme":"signalgelb","users":[{"name":"anna","template":"view","password":"kurz"}]}`)
	if err := f.apply(t); err == nil {
		t.Fatal("partial failure returned nil, want an error")
	}
	if !f.fileGone(t) {
		t.Fatal("seed file still present after partial failure")
	}
	set, _ := f.settings.LoadSettings()
	if set.Theme != settings.ThemeSignalgelb {
		t.Fatalf("theme = %q, want the rest of the seed applied", set.Theme)
	}
}

func TestSeedNeverLogsPasswords(t *testing.T) {
	f := newSeedFixture(t)
	f.write(t, `{"version":1,"users":[`+
		`{"name":"anna","template":"view","password":"`+seedPassword+`"},`+
		`{"name":"bert","template":"unbekannt","password":"`+seedPassword+`-2"},`+
		`{"name":"AB","template":"operate","password":"kurz-geheim"}],`+
		`"theme":"tageslicht","guest_access":true}`)
	_ = f.apply(t)
	for _, secret := range []string{seedPassword, seedPassword + "-2", "kurz-geheim"} {
		if strings.Contains(f.logs.String(), secret) {
			t.Fatalf("log contains password %q: %s", secret, f.logs.String())
		}
	}
}

func TestSeedUnknownVersionIsIgnoredAndLogged(t *testing.T) {
	f := newSeedFixture(t)
	f.write(t, `{"version":2,"theme":"stromblau","users":[{"name":"anna","template":"view","password":"`+seedPassword+`"}]}`)
	if err := f.apply(t); err != nil {
		t.Fatalf("unknown version returned %v, want nil", err)
	}
	if !f.fileGone(t) {
		t.Fatal("seed file with unknown version still present")
	}
	if f.logs.Len() == 0 {
		t.Fatal("unknown version not logged")
	}
	set, _ := f.settings.LoadSettings()
	if set.Theme != settings.Default().Theme {
		t.Fatalf("theme = %q, unknown version must not apply anything", set.Theme)
	}
	if slices.Contains(f.userNames(), "anna") {
		t.Fatal("user created from a seed with unknown version")
	}
}

func TestSeedCreatesUsersWithTemplateRoles(t *testing.T) {
	f := newSeedFixture(t)
	f.write(t, `{"version":1,"users":[`+
		`{"name":"vera","template":"view","password":"`+seedPassword+`"},`+
		`{"name":"olaf","template":"operate","password":"`+seedPassword+`"},`+
		`{"name":"adam","template":"admin","password":"`+seedPassword+`"}]}`)
	if err := f.apply(t); err != nil {
		t.Fatal(err)
	}
	roles := map[string][]string{}
	for _, user := range f.mgr.Users() {
		roles[user.Username] = user.Roles
	}
	if !slices.Contains(roles["olaf"], auth.RoleOperateDevices) || !slices.Contains(roles["olaf"], auth.RoleOperateAutomations) {
		t.Fatalf("operate roles = %v", roles["olaf"])
	}
	if slices.Contains(roles["vera"], auth.RoleOperateDevices) {
		t.Fatalf("view user got operate rights: %v", roles["vera"])
	}
	if !slices.Contains(roles["adam"], auth.RoleUserManagement) {
		t.Fatalf("admin template lacks user management: %v", roles["adam"])
	}
	if slices.Contains(roles["adam"], auth.RoleHTTPLogin) {
		t.Fatal("admin template must not carry http_login, like the auth.pw account")
	}
}

func TestSeedMarksStepsInstaller(t *testing.T) {
	f := newSeedFixture(t)
	f.write(t, `{"version":1,"theme":"stromblau","title":"Haus","metrics":["cpu_temp"],`+
		`"users":[{"name":"anna","template":"view","password":"`+seedPassword+`"}]}`)
	if err := f.apply(t); err != nil {
		t.Fatal(err)
	}
	steps := f.ob.Load().SetupSteps
	for _, id := range []string{"users", "theme", "title", "metrics"} {
		if steps[id] != StepInstaller {
			t.Errorf("%s = %q, want %q", id, steps[id], StepInstaller)
		}
	}
	if _, ok := steps["guest"]; ok {
		t.Errorf("guest marked although the seed left it out: %v", steps)
	}
}

func TestSeedLaterMarksStepLater(t *testing.T) {
	f := newSeedFixture(t)
	f.write(t, `{"version":1,"later":["theme","guest","nicht_da"]}`)
	if err := f.apply(t); err != nil {
		t.Fatal(err)
	}
	steps := f.ob.Load().SetupSteps
	if steps["theme"] != StepLater || steps["guest"] != StepLater {
		t.Fatalf("steps = %v, want theme and guest later", steps)
	}
	if _, ok := steps["nicht_da"]; ok {
		t.Fatal("unknown later id stored as a step")
	}
	set, _ := f.settings.LoadSettings()
	if set.Theme != settings.Default().Theme {
		t.Fatalf("later group changed theme to %q", set.Theme)
	}
}
