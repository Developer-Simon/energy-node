// Package faults bildet die stabilen Fehlercodes der Bootstrap-Skripte auf
// Klartext und Handlungsempfehlung ab. Die Codes stehen hier als Go-Konstanten
// - sie sind Vertrag zwischen Bash und Go, ein Tippfehler soll ein
// Compilerfehler sein. Die Texte stehen im Nachrichtenkatalog des webui-Moduls,
// unter fault.<CODE>.message und fault.<CODE>.remediation; das ist dieselbe
// Quelle, aus der die Oberflaeche uebersetzt, und es gibt sie nur einmal.
package faults

import (
	"strings"
	"sync"

	webui "github.com/Developer-Simon/energy-node-webui"
	"github.com/Developer-Simon/energy-node-webui/i18n"
)

// Code ist ein Fehlercode, wie ihn ein Bootstrap-Skript nach
// "##STEP <id> fail " schreibt.
type Code string

// Die bekannten Codes. Quelle: die "Fehlercodes"-Zeilen der Plaene A-I/A-II
// und die fuenf Bundle-Codes aus Plan B-I.
const (
	CodeBundleManifestMissing   Code = "BUNDLE_MANIFEST_MISSING"
	CodeBundleSignatureInvalid  Code = "BUNDLE_SIGNATURE_INVALID"
	CodeBundleHashMismatch      Code = "BUNDLE_HASH_MISMATCH"
	CodeArchMismatch            Code = "ARCH_MISMATCH"
	CodePythonABIMismatch       Code = "PYTHON_ABI_MISMATCH"
	CodeAptFailed               Code = "APT_FAILED"
	CodeMosquittoArgsMissing    Code = "MOSQUITTO_ARGS_MISSING"
	CodeMosquittoConfigInvalid  Code = "MOSQUITTO_CONFIG_INVALID"
	CodeMQTTConfigUnreadable    Code = "MQTT_CONFIG_UNREADABLE"
	CodeUFWMissing              Code = "UFW_MISSING"
	CodePipExternallyManaged    Code = "PIP_EXTERNALLY_MANAGED"
	CodeWheelMissing            Code = "WHEEL_MISSING"
	CodeTailscaleFlagInvalid    Code = "TAILSCALE_FLAG_INVALID"
	CodeCaddyValidateFailed     Code = "CADDY_VALIDATE_FAILED"
	CodeUnitStartFailed         Code = "UNIT_START_FAILED"
	CodeConfigExists            Code = "CONFIG_EXISTS"
	CodeSudoRequired            Code = "SUDO_REQUIRED"
	CodeAptInstallFailed        Code = "APT_INSTALL_FAILED"
	CodeAptUpdateFailed         Code = "APT_UPDATE_FAILED"
	CodeBundleIncomplete        Code = "BUNDLE_INCOMPLETE"
	CodeCaddyBinaryMissing      Code = "CADDY_BINARY_MISSING"
	CodeCaddyConfigInvalid      Code = "CADDY_CONFIG_INVALID"
	CodeCaddyStartFailed        Code = "CADDY_START_FAILED"
	CodeConfigJsonMissing       Code = "CONFIG_JSON_MISSING"
	CodeConfigTemplateMissing   Code = "CONFIG_TEMPLATE_MISSING"
	CodeConfigWriteFailed       Code = "CONFIG_WRITE_FAILED"
	CodeDashboardBinaryMissing  Code = "DASHBOARD_BINARY_MISSING"
	CodeDashboardStartFailed    Code = "DASHBOARD_START_FAILED"
	CodeManifestsMissing        Code = "MANIFESTS_MISSING"
	CodeManifestMissing         Code = "MANIFEST_MISSING"
	CodeManifestParseFailed     Code = "MANIFEST_PARSE_FAILED"
	CodeMosquittoConfForeign    Code = "MOSQUITTO_CONF_FOREIGN"
	CodeMosquittoPasswdFailed   Code = "MOSQUITTO_PASSWD_FAILED"
	CodePipInstallFailed        Code = "PIP_INSTALL_FAILED"
	CodeSecretFileMissing       Code = "SECRET_FILE_MISSING"
	CodeSelectionUnreadable     Code = "SELECTION_UNREADABLE"
	CodeServiceSourceMissing    Code = "SERVICE_SOURCE_MISSING"
	CodeServiceStartFailed      Code = "SERVICE_START_FAILED"
	CodeServiceUnitFailed       Code = "SERVICE_UNIT_FAILED"
	CodeSudoersInvalid          Code = "SUDOERS_INVALID"
	CodeTailscaleInstallFailed  Code = "TAILSCALE_INSTALL_FAILED"
	CodeTailscaleTarballMissing Code = "TAILSCALE_TARBALL_MISSING"
	CodeTargetInvalid           Code = "TARGET_INVALID"
	CodeUfwFailed               Code = "UFW_FAILED"
	CodeUpdaterPathStartFailed  Code = "UPDATER_PATH_START_FAILED"
	CodeWheelsMissing           Code = "WHEELS_MISSING"
)

var allCodes = []Code{
	CodeAptFailed,
	CodeAptInstallFailed,
	CodeAptUpdateFailed,
	CodeArchMismatch,
	CodeBundleHashMismatch,
	CodeBundleIncomplete,
	CodeBundleManifestMissing,
	CodeBundleSignatureInvalid,
	CodeCaddyBinaryMissing,
	CodeCaddyConfigInvalid,
	CodeCaddyStartFailed,
	CodeCaddyValidateFailed,
	CodeConfigExists,
	CodeConfigJsonMissing,
	CodeConfigTemplateMissing,
	CodeConfigWriteFailed,
	CodeDashboardBinaryMissing,
	CodeDashboardStartFailed,
	CodeManifestsMissing,
	CodeManifestMissing,
	CodeManifestParseFailed,
	CodeMosquittoArgsMissing,
	CodeMosquittoConfigInvalid,
	CodeMosquittoConfForeign,
	CodeMosquittoPasswdFailed,
	CodeMQTTConfigUnreadable,
	CodePipExternallyManaged,
	CodePipInstallFailed,
	CodePythonABIMismatch,
	CodeSecretFileMissing,
	CodeSelectionUnreadable,
	CodeServiceSourceMissing,
	CodeServiceStartFailed,
	CodeServiceUnitFailed,
	CodeSudoersInvalid,
	CodeSudoRequired,
	CodeTailscaleFlagInvalid,
	CodeTailscaleInstallFailed,
	CodeTailscaleTarballMissing,
	CodeTargetInvalid,
	CodeUfwFailed,
	CodeUFWMissing,
	CodeUnitStartFailed,
	CodeUpdaterPathStartFailed,
	CodeWheelsMissing,
	CodeWheelMissing,
}

// Entry ist ein Fehlercode samt Klartext in der aktiven Sprache.
type Entry struct {
	Code        Code
	Message     string
	Remediation string
}

var (
	mu       sync.RWMutex
	language = i18n.Fallback
	loadOnce sync.Once
	catalogs *i18n.Set
	loadErr  error
)

func set() *i18n.Set {
	loadOnce.Do(func() {
		catalogs, loadErr = i18n.Load(webui.Catalogs())
	})
	return catalogs
}

// SetLanguage waehlt die Sprache der Klartexte. Vorgabe ist Englisch; die
// Entwickler-CLI ruft das mit der OS-Locale auf, der Dashboard-Wirt fest mit
// "de", und die Oberflaeche gar nicht - sie uebersetzt selbst.
func SetLanguage(lang string) {
	mu.Lock()
	defer mu.Unlock()
	language = lang
}

// Language liefert die aktive Sprache.
func Language() string {
	mu.RLock()
	defer mu.RUnlock()
	return language
}

// Codes liefert alle bekannten Codes, sortiert.
func Codes() []Code {
	out := make([]Code, len(allCodes))
	copy(out, allCodes)
	return out
}

// Lookup liefert den Klartext zu einem Code in der aktiven Sprache. ok ist
// false, wenn der Code unbekannt ist - dann ist Unknown zustaendig.
func Lookup(code string) (Entry, bool) {
	known := false
	for _, candidate := range allCodes {
		if string(candidate) == code {
			known = true
			break
		}
	}
	if !known {
		return Entry{}, false
	}
	if set() == nil {
		return Entry{Code: Code(code)}, true
	}
	lang := Language()
	message, _ := set().Lookup(lang, "fault."+code+".message")
	remediation, _ := set().Lookup(lang, "fault."+code+".remediation")
	return Entry{Code: Code(code), Message: message, Remediation: remediation}, true
}

// Unknown baut einen Eintrag fuer einen Code, den dieses Programm nicht kennt.
// Die Spec verlangt ausdruecklich, dass ein solcher Code als unbekannt gezeigt
// wird - zusammen mit den letzten Logzeilen des Schritts, um die sich der
// Aufrufer kuemmert.
func Unknown(code string) Entry {
	lang := Language()
	message := "Unknown error code " + code + "."
	if s := set(); s != nil {
		if text, ok := s.Lookup(lang, "fault.unknown.message"); ok && text != "" {
			message = strings.ReplaceAll(text, "{code}", code)
		}
	}
	return Entry{Code: Code(code), Message: message}
}

// LoadError meldet, ob die Kataloge ueberhaupt lesbar waren. Ein Aufrufer, der
// das ignoriert, bekommt leere Texte statt einer Panik - eine kaputte
// Uebersetzung darf einen laufenden Deploy nicht abbrechen.
func LoadError() error {
	set()
	return loadErr
}
