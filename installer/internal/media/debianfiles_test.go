package media

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files in testdata/debian/golden")

// fixedHostKey keeps the goldens deterministic, the real key is random.
var fixedHostKey = HostKey{
	PrivatePEM: []byte("-----BEGIN OPENSSH PRIVATE KEY-----\ntest\n-----END OPENSSH PRIVATE KEY-----\n"),
	PublicLine: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAATEST energy-node",
}

func testDebianSettings(arch, installLang, systemLang string) DebianSettings {
	return DebianSettings{
		Settings: Settings{
			Hostname:      "energy-node",
			User:          "node",
			AuthorizedKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAINSTALLER installer",
			Timezone:      "Europe/Berlin",
			Keyboard:      "de",
		},
		Arch:           arch,
		AskBeforeErase: true,
		InstallLang:    installLang,
		SystemLang:     systemLang,
	}
}

// checkGolden compares got with testdata/debian/golden/name, or rewrites the
// file when the test runs with -update.
func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "debian", "golden", name)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s: %v (run go test -update)", name, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from the golden file, review and run go test -update\n--- got ---\n%s", name, got)
	}
}

func debianFile(t *testing.T, files DebianFiles, name string) []byte {
	t.Helper()
	for _, f := range files.Add {
		if f.Name == name {
			return f.Data
		}
	}
	t.Fatalf("no file %s in Add", name)
	return nil
}

func buildDebian(t *testing.T, s DebianSettings) DebianFiles {
	t.Helper()
	files, err := BuildDebianFiles(s, fixedHostKey)
	if err != nil {
		t.Fatalf("BuildDebianFiles: %v", err)
	}
	return files
}

func hasLanguageLines(preseed []byte) bool {
	return bytes.Contains(preseed, []byte("debian-installer/language string")) ||
		bytes.Contains(preseed, []byte("debian-installer/country string")) ||
		bytes.Contains(preseed, []byte("debian-installer/locale string"))
}

func TestBuildDebianFilesAddsTheStickFiles(t *testing.T) {
	files := buildDebian(t, testDebianSettings("amd64", "de", "de"))
	var names []string
	for _, f := range files.Add {
		names = append(names, f.Name)
	}
	want := "preseed.cfg energy-node.sh energy-node.tpl energy-node.env energy-node.pub energy-node.key energy-node.hpub"
	if got := strings.Join(names, " "); got != want {
		t.Fatalf("Add = %q, want %q", got, want)
	}
	if _, ok := files.Replace["/isolinux/isolinux.cfg"]; !ok {
		t.Fatal("amd64 must replace the isolinux menu")
	}
	arm := buildDebian(t, testDebianSettings("arm64", "en", "en"))
	if _, ok := arm.Replace["/isolinux/isolinux.cfg"]; ok {
		t.Fatal("arm64 must not replace the isolinux menu")
	}
}

func TestPreseedAmd64German(t *testing.T) {
	files := buildDebian(t, testDebianSettings("amd64", "de", "de"))
	preseed := debianFile(t, files, "preseed.cfg")
	for _, line := range []string{
		"d-i debian-installer/language string de\n",
		"d-i debian-installer/country string DE\n",
		"d-i debian-installer/locale string de_DE.UTF-8\n",
		"d-i localechooser/supported-locales multiselect de_DE.UTF-8\n",
	} {
		if !bytes.Contains(preseed, []byte(line)) {
			t.Errorf("preseed lacks %q", line)
		}
	}
	checkGolden(t, "preseed-amd64-de.cfg", preseed)
}

func TestPreseedAmd64English(t *testing.T) {
	files := buildDebian(t, testDebianSettings("amd64", "en", "en"))
	preseed := debianFile(t, files, "preseed.cfg")
	if hasLanguageLines(preseed) {
		t.Fatal("amd64 with English must not set the installer language lines")
	}
	if !bytes.Contains(preseed, []byte("localechooser/supported-locales multiselect en_US.UTF-8\n")) {
		t.Fatal("supported-locales must follow SystemLang")
	}
	checkGolden(t, "preseed-amd64-en.cfg", preseed)
}

func TestPreseedArm64HasNoLanguageLinesEvenWhenGermanIsAsked(t *testing.T) {
	files := buildDebian(t, testDebianSettings("arm64", "de", "de"))
	preseed := debianFile(t, files, "preseed.cfg")
	if hasLanguageLines(preseed) {
		t.Fatal("arm64 must not set the installer language lines, even with InstallLang de")
	}
	checkGolden(t, "preseed-arm64-de-asked.cfg", preseed)
}

func TestEnvFileReflectsTheSwitches(t *testing.T) {
	defaults := testDebianSettings("amd64", "de", "de")
	switches := testDebianSettings("amd64", "en", "en")
	switches.AskBeforeErase = false
	switches.SSHPassword = true
	switches.PasswordHash = "$6$saltsalt$AbCdEf0123456789./AbCdEf0123456789./AbCdEf0123456789./AbCdEf0123456789./AbCd"

	for _, tc := range []struct {
		name   string
		s      DebianSettings
		golden string
		want   []string
	}{
		{"defaults", defaults, "env-defaults.env", []string{
			"EN_USER='node'\n", "EN_PASSWORD_LOGIN=no\n", "EN_CONFIRM=yes\n", "EN_TARGET_LOCALE='de_DE.UTF-8'\n",
		}},
		{"switches", switches, "env-switches.env", []string{
			"EN_PASSWORD_LOGIN=yes\n", "EN_CONFIRM=no\n", "EN_TARGET_LOCALE='en_US.UTF-8'\n",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := debianFile(t, buildDebian(t, tc.s), "energy-node.env")
			for _, line := range tc.want {
				if !bytes.Contains(env, []byte(line)) {
					t.Errorf("env lacks %q:\n%s", line, env)
				}
			}
			checkGolden(t, tc.golden, env)
		})
	}

	noHash := switches
	noHash.PasswordHash = ""
	if env := debianFile(t, buildDebian(t, noHash), "energy-node.env"); !bytes.Contains(env, []byte("EN_PASSWORD_LOGIN=no\n")) {
		t.Errorf("SSHPassword without a hash must not enable password login:\n%s", env)
	}
}

func TestMenusPointAtThePreseed(t *testing.T) {
	cases := []struct {
		name  string
		s     DebianSettings
		menus map[string]string // menu file to golden name
	}{
		{"amd64-de", testDebianSettings("amd64", "de", "de"), map[string]string{
			"/boot/grub/grub.cfg":    "grub-amd64-de.cfg",
			"/isolinux/isolinux.cfg": "isolinux-amd64-de.cfg",
		}},
		{"arm64-en", testDebianSettings("arm64", "en", "en"), map[string]string{
			"/boot/grub/grub.cfg": "grub-arm64-en.cfg",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := buildDebian(t, tc.s)
			for path, golden := range tc.menus {
				menu, ok := files.Replace[path]
				if !ok {
					t.Fatalf("no replacement for %s", path)
				}
				if got := bytes.Count(menu, []byte("file=/cdrom/preseed.cfg")); got != 1 {
					t.Errorf("%s: %d preseed references, want 1", path, got)
				}
				if got := bytes.Count(menu, []byte("auto=true priority=critical")); got != 1 {
					t.Errorf("%s: %d automatic entries, want 1", path, got)
				}
				checkGolden(t, golden, menu)
			}
			grub := files.Replace["/boot/grub/grub.cfg"]
			if !bytes.Contains(grub, []byte("set default=0\nset timeout=10\n")) {
				t.Errorf("grub must default to entry 0 with a 10 s timeout")
			}
			if isolinux, ok := files.Replace["/isolinux/isolinux.cfg"]; ok {
				if !bytes.Contains(isolinux, []byte("timeout 100\n")) || !bytes.Contains(isolinux, []byte("label energynode\n\tmenu label ^Energy Node: Debian installieren (loescht die interne Platte)\n\tmenu default\n")) {
					t.Errorf("isolinux must boot the energy node entry by default after 10 s")
				}
			}
		})
	}
}

func TestSettingsWithNewlinesCannotInjectPreseedLines(t *testing.T) {
	bad := map[string]func(*DebianSettings){
		"hostname with preseed line": func(s *DebianSettings) {
			s.Hostname = "a\nd-i partman/early_command string x"
		},
		"user with quote": func(s *DebianSettings) {
			s.User = `node"`
		},
		"authorized key with newline": func(s *DebianSettings) {
			s.AuthorizedKey = "ssh-ed25519 AAAA\nd-i partman/early_command string x"
		},
		"timezone with newline": func(s *DebianSettings) {
			s.Timezone = "Europe/Berlin\nd-i partman/early_command string x"
		},
		"keyboard with newline": func(s *DebianSettings) {
			s.Keyboard = "de\nd-i partman/early_command string x"
		},
		"password hash with newline": func(s *DebianSettings) {
			s.PasswordHash = "$6$salt$hash\nd-i partman/early_command string x"
		},
		"password hash not sha512": func(s *DebianSettings) {
			s.PasswordHash = "plain text"
		},
		"unknown arch": func(s *DebianSettings) { s.Arch = "386" },
		"unknown install language": func(s *DebianSettings) {
			s.InstallLang = "de\nd-i x"
		},
		"unknown system language": func(s *DebianSettings) { s.SystemLang = "fr" },
	}
	for name, mutate := range bad {
		t.Run(name, func(t *testing.T) {
			s := testDebianSettings("amd64", "de", "de")
			mutate(&s)
			if _, err := BuildDebianFiles(s, fixedHostKey); err == nil {
				t.Fatal("BuildDebianFiles accepted the setting")
			}
		})
	}
}
