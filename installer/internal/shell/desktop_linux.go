//go:build linux

package shell

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed assets/icon.png
var iconPNG []byte

// installDesktopEntry gives the window its icon. Wayland compositors (and
// most docks) take a window's icon from the .desktop file whose name matches
// the window's app id, never from the page's favicon. GTK derives the app id
// from the program name, so the entry is named after the running binary. The
// installer ships as a bare binary, hence the entry and the icon are written
// into the user's data directory on every start. Existing files with the same
// content are left alone.
func installDesktopEntry(dataHome, exe, name string) error {
	icon := filepath.Join(dataHome, "icons", "hicolor", "256x256", "apps", name+".png")
	entry := filepath.Join(dataHome, "applications", name+".desktop")
	if err := writeIfChanged(icon, iconPNG); err != nil {
		return err
	}
	return writeIfChanged(entry, []byte(desktopEntry(exe, name, icon)))
}

func desktopEntry(exe, name, icon string) string {
	return fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=%s
Exec=%s
Icon=%s
Terminal=false
Categories=Utility;
StartupWMClass=%s
`, windowTitle, quoteExec(exe), icon, name)
}

// quoteExec quotes a path for the Exec key. The spec asks for double quotes
// and a backslash before the characters that stay special inside them.
func quoteExec(path string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`)
	return `"` + strings.ReplaceAll(r.Replace(path), "%", "%%") + `"`
}

func writeIfChanged(path string, data []byte) error {
	if old, err := os.ReadFile(path); err == nil && string(old) == string(data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// registerDesktopEntry is best effort: a missing icon is cosmetic and must
// never stop the installer from opening its window.
func registerDesktopEntry() {
	exe, err := os.Executable()
	if err != nil || len(os.Args) == 0 {
		return
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	_ = installDesktopEntry(dataHome, exe, filepath.Base(os.Args[0]))
}
