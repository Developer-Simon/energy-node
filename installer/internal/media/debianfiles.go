package media

import (
	"bytes"
	_ "embed"
	"fmt"
	"regexp"
	"strings"
	"text/template"
	"unicode"

	"github.com/Developer-Simon/energy-node-installer/internal/media/isopatch"
)

// DebianSettings is everything the installer writes onto a Debian netinst
// stick: the first boot settings plus the switches of the unattended install.
type DebianSettings struct {
	Settings              // Hostname, User, AuthorizedKey, PasswordHash, SSHPassword, Timezone, Keyboard
	Arch           string // "amd64" | "arm64"
	AskBeforeErase bool   // Schalter "Vor dem Löschen nachfragen"
	InstallLang    string // "de" | "en", nur amd64 wirksam
	SystemLang     string // "de" | "en", Systemsprache des Zielsystems
}

// DebianFiles are the changes made to a Debian netinst ISO.
type DebianFiles struct {
	Add     []isopatch.File   // preseed.cfg, energy-node.sh, energy-node.tpl, energy-node.env, energy-node.pub, energy-node.key, energy-node.hpub
	Replace map[string][]byte // /boot/grub/grub.cfg und nur bei amd64 /isolinux/isolinux.cfg
}

//go:embed debiantpl/preseed.cfg.tmpl
var debianPreseedTemplate string

//go:embed debiantpl/grub.cfg.tmpl
var debianGrubTemplate string

//go:embed debiantpl/isolinux.cfg.tmpl
var debianIsolinuxTemplate string

//go:embed debiantpl/energy-node.sh
var debianScript []byte

//go:embed debiantpl/energy-node.tpl
var debianMessages []byte

var (
	preseedTmpl  = template.Must(template.New("preseed").Parse(debianPreseedTemplate))
	grubTmpl     = template.Must(template.New("grub").Parse(debianGrubTemplate))
	isolinuxTmpl = template.Must(template.New("isolinux").Parse(debianIsolinuxTemplate))

	keyboardPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	timezonePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+)*$`)
	hashPattern     = regexp.MustCompile(`^\$6\$[A-Za-z0-9./]+\$[A-Za-z0-9./]+$`)
)

// debianView is the data the templates are rendered with.
type debianView struct {
	DebianSettings
	LangLines     bool   // the three debian-installer/language|country|locale lines
	Locale        string // locale of the installer menus
	SystemLocale  string // locale of the installed system
	Amd64         bool
	BootDir       string // install.amd or install.a64
	PasswordField string // sha512-crypt hash, or "!" for a locked account
}

// BuildDebianFiles renders the preseed, the menus, the helper script and its
// environment for a Debian stick. Invalid settings are refused before any
// value reaches a preseed line.
func BuildDebianFiles(s DebianSettings, hk HostKey) (DebianFiles, error) {
	if err := validateDebianSettings(s); err != nil {
		return DebianFiles{}, err
	}
	amd64 := s.Arch == "amd64"
	v := debianView{
		DebianSettings: s,
		LangLines:      amd64 && s.InstallLang == "de",
		Locale:         "en_US.UTF-8",
		SystemLocale:   localeFor(s.SystemLang),
		Amd64:          amd64,
		BootDir:        "install.a64",
		PasswordField:  "!",
	}
	if v.LangLines {
		v.Locale = "de_DE.UTF-8"
	}
	if amd64 {
		v.BootDir = "install.amd"
	}
	if s.PasswordHash != "" {
		v.PasswordField = s.PasswordHash
	}

	preseed, err := render(preseedTmpl, v)
	if err != nil {
		return DebianFiles{}, err
	}
	grub, err := render(grubTmpl, v)
	if err != nil {
		return DebianFiles{}, err
	}
	files := DebianFiles{
		Add: []isopatch.File{
			{Name: "preseed.cfg", Data: preseed},
			{Name: "energy-node.sh", Data: debianScript},
			{Name: "energy-node.tpl", Data: debianMessages},
			{Name: "energy-node.env", Data: debianEnv(s)},
			{Name: "energy-node.pub", Data: []byte(s.AuthorizedKey + "\n")},
			{Name: "energy-node.key", Data: hk.PrivatePEM},
			{Name: "energy-node.hpub", Data: []byte(hk.PublicLine + "\n")},
		},
		Replace: map[string][]byte{"/boot/grub/grub.cfg": grub},
	}
	if amd64 {
		isolinux, err := render(isolinuxTmpl, v)
		if err != nil {
			return DebianFiles{}, err
		}
		files.Replace["/isolinux/isolinux.cfg"] = isolinux
	}
	return files, nil
}

func render(t *template.Template, v debianView) ([]byte, error) {
	var b bytes.Buffer
	if err := t.Execute(&b, v); err != nil {
		return nil, fmt.Errorf("media: render %s: %w", t.Name(), err)
	}
	return b.Bytes(), nil
}

// localeFor maps "de" or "en" to the system locale of the target.
func localeFor(lang string) string {
	if lang == "de" {
		return "de_DE.UTF-8"
	}
	return "en_US.UTF-8"
}

// debianEnv renders energy-node.env, which the helper script sources. Every
// value is shell-quoted, so the file stays one assignment per line.
func debianEnv(s DebianSettings) []byte {
	confirm := "no"
	if s.AskBeforeErase {
		confirm = "yes"
	}
	passwordLogin := "no"
	if s.SSHPassword && s.PasswordHash != "" {
		passwordLogin = "yes"
	}
	var b strings.Builder
	b.WriteString("EN_USER=" + shellQuote(s.User) + "\n")
	b.WriteString("EN_PASSWORD_LOGIN=" + passwordLogin + "\n")
	b.WriteString("EN_CONFIRM=" + confirm + "\n")
	b.WriteString("EN_TARGET_LOCALE=" + shellQuote(localeFor(s.SystemLang)) + "\n")
	return []byte(b.String())
}

// validateDebianSettings refuses every value that could change the shape of
// the preseed or the environment file. Newlines and control characters are
// never accepted, so one setting cannot inject a second preseed line.
func validateDebianSettings(s DebianSettings) error {
	switch {
	case !ValidHostname(s.Hostname):
		return fmt.Errorf("media: invalid hostname %q", s.Hostname)
	case !ValidUser(s.User):
		return fmt.Errorf("media: invalid user %q", s.User)
	case s.Arch != "amd64" && s.Arch != "arm64":
		return fmt.Errorf("media: invalid arch %q", s.Arch)
	case s.InstallLang != "de" && s.InstallLang != "en":
		return fmt.Errorf("media: invalid install language %q", s.InstallLang)
	case s.SystemLang != "de" && s.SystemLang != "en":
		return fmt.Errorf("media: invalid system language %q", s.SystemLang)
	case !keyboardPattern.MatchString(s.Keyboard):
		return fmt.Errorf("media: invalid keyboard %q", s.Keyboard)
	case !timezonePattern.MatchString(s.Timezone):
		return fmt.Errorf("media: invalid timezone %q", s.Timezone)
	case s.AuthorizedKey == "" || strings.IndexFunc(s.AuthorizedKey, unicode.IsControl) >= 0:
		return fmt.Errorf("media: authorized key must be one line")
	case s.PasswordHash != "" && !hashPattern.MatchString(s.PasswordHash):
		return fmt.Errorf("media: password hash is not sha512-crypt")
	}
	return nil
}
