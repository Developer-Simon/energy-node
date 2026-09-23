package hostapi

import "context"

// HostKind sagt der Oberflaeche, in welchem Wirt sie laeuft. Sie soll daraus
// nur ableiten, was sie zeigt (etwa den Verbindungsbildschirm), nie, wie sie
// mit Schicht 2 redet - das ist in beiden Faellen identisch.
type HostKind string

const (
	HostInstaller HostKind = "installer"
	HostDashboard HostKind = "dashboard"
)

// Description beschreibt den Wirt. Sie geht als Teil von /api/bootstrap hinaus.
type Description struct {
	Host            HostKind `json:"host"`
	EntryPoints     []string `json:"entry_points"`
	NeedsConnection bool     `json:"needs_connection"`
	BundleVersion   string   `json:"bundle_version"`
	// BundleArch ist die Zielarchitektur des Bundles (manifest.arch). Der
	// Verbindungsbildschirm zeigt sie, bevor eine Verbindung besteht.
	BundleArch string `json:"bundle_arch"`
	// Package macht die Paketauswahl auf dem Verbindungsbildschirm moeglich.
	// nil heisst: dieser Wirt hat keine.
	Package *PackageInfo `json:"package,omitempty"`
	// AutoPrepare sagt: dieser Wirt besorgt sein Paket selbst (das Dashboard
	// laedt es von GitHub). Die Shell zeigt dann vor dem ersten Bildschirm des
	// Redeploy den Bildschirm "Paket vorbereiten", der POST /api/run mit
	// mode "prepare" ausloest.
	AutoPrepare bool `json:"auto_prepare,omitempty"`
}

// AuthKind ist die Art, wie sich der Installer am Node anmeldet.
type AuthKind string

const (
	AuthPassword AuthKind = "password"
	AuthKey      AuthKind = "key"
	AuthAgent    AuthKind = "agent"
)

// ConnectRequest ist der Rumpf von POST /api/connect.
type ConnectRequest struct {
	Host string   `json:"host"`
	User string   `json:"user"`
	Kind AuthKind `json:"kind"`
	// Secret ist das Passwort bzw. die Passphrase. Es geht nur hinein, nie
	// hinaus - keine Antwort dieses Pakets traegt es zurueck.
	Secret string `json:"secret"`
	// KeyPath ist der Pfad zu einer privaten Schluesseldatei auf dem Rechner
	// des Betreibers (Kind == AuthKey).
	KeyPath string `json:"key_path"`
	// AcceptFingerprint ist der Fingerabdruck, den der Betreiber im
	// TOFU-Dialog bestaetigt hat. Leer beim ersten Versuch.
	AcceptFingerprint string `json:"accept_fingerprint"`
}

// ConnectResult meldet den Ausgang eines Verbindungsversuchs.
type ConnectResult struct {
	Connected bool   `json:"connected"`
	Host      string `json:"host"`
	User      string `json:"user"`
	// Fingerprint ist gesetzt, wenn der Host-Key unbekannt ist und der
	// Betreiber ihn bestaetigen soll.
	Fingerprint string `json:"fingerprint,omitempty"`
}

// KeypairResult beschreibt ein frisch erzeugtes und auf dem Node hinterlegtes
// Schluesselpaar.
type KeypairResult struct {
	PublicKey   string `json:"public_key"`
	PrivatePath string `json:"private_path"`
	Installed   bool   `json:"installed"`
}

// Precheck ist die Vorpruefung (Vertrag 4 plus der Vergleich gegen das
// Manifest). Kein Feld dieses Typs veraendert etwas auf dem Node.
type Precheck struct {
	OSID                   string `json:"os_id"`
	OSVersionID            string `json:"os_version_id"`
	Arch                   string `json:"arch"`
	PythonABI              string `json:"python_abi"`
	PythonVersion          string `json:"python_version"`
	DiskFreeMB             int64  `json:"disk_free_mb"`
	SudoNopasswd           bool   `json:"sudo_nopasswd"`
	Internet               bool   `json:"internet"`
	Installed              bool   `json:"installed"`
	InstalledBundleVersion string `json:"installed_bundle_version"`
	// OSPrettyName ist PRETTY_NAME aus /etc/os-release, leer wenn unbekannt.
	OSPrettyName string `json:"os_pretty_name"`
	// DiskTotalMB ist die Groesse des Dateisystems, auf dem DiskFreeMB
	// gemessen wurde; 0 heisst unbekannt.
	DiskTotalMB int64 `json:"disk_total_mb"`
	// Timezone ist die Zeitzone des Node, etwa "Europe/Berlin".
	Timezone string `json:"timezone"`
	// Die Bewertung. Codes stammen aus internal/faults und sind sprachneutral;
	// die Oberflaeche uebersetzt sie.
	ArchOK      bool     `json:"arch_ok"`
	PythonABIOK bool     `json:"python_abi_ok"`
	DiskOK      bool     `json:"disk_ok"`
	Blocking    []string `json:"blocking"`
	Warnings    []string `json:"warnings"`
}

// StepView ist ein Schritt, wie ihn die Oberflaeche zeigt.
type StepView struct {
	ID        string `json:"id"`
	ServiceID string `json:"service_id,omitempty"`
	Dir       string `json:"dir,omitempty"`
	Unit      string `json:"unit,omitempty"`
	Optional  bool   `json:"optional"`
	Default   bool   `json:"default"`
	// Kind ist "device" fuer einen Geraete-Dienst, "service" fuer einen
	// anderen Python-Dienst und leer fuer einen Systemschritt.
	Kind string `json:"kind,omitempty"`
	// Requires ist der Schritt, ohne den dieser nicht laeuft (manifest.json,
	// "requires"). Die Konfiguration sperrt den Schalter, solange der
	// benoetigte Schritt aus ist.
	Requires string `json:"requires,omitempty"`
}

// ManifestView ist der fuer die Oberflaeche interessante Teil des Manifests.
type ManifestView struct {
	BundleVersion string            `json:"bundle_version"`
	Arch          string            `json:"arch"`
	PythonABI     string            `json:"python_abi"`
	TargetUser    string            `json:"target_user"`
	TargetBase    string            `json:"target_base"`
	Components    map[string]string `json:"components"`
	Steps         []StepView        `json:"steps"`
	HasCaddy      bool              `json:"has_caddy"`
	// BundleBytes ist die Summe der Dateigroessen aus manifest.files.
	BundleBytes int64 `json:"bundle_bytes"`
	// WheelCount, UnitCount und TemplateCount zaehlen die Dateien unter
	// wheels/, die *.service-Dateien und die Dateien unter config/.
	WheelCount    int `json:"wheel_count"`
	UnitCount     int `json:"unit_count"`
	TemplateCount int `json:"template_count"`
}

// SelectionView ist die aktuelle Dienstauswahl samt ihrer Herkunft.
type SelectionView struct {
	Steps map[string]bool `json:"steps"`
	// Source ist "node" (selection.json lag auf dem Geraet) oder
	// "manifest-default" (Erstinstallation: die Vorgaben des Manifests).
	Source string `json:"source"`
}

// ComponentDelta ist das "von"/"nach" einer Komponente. From ist ein Zeiger,
// weil plan.sh null meldet, wenn es keinen Vorzustand gibt - "unbekannt" ist
// etwas anderes als "leer".
type ComponentDelta struct {
	From *string `json:"from"`
	To   string  `json:"to"`
}

// PlanStep ist ein Schritt in der Vorschau. State ist "pending", "done" oder
// "deselected", wie plan.sh es meldet.
type PlanStep struct {
	ID       string `json:"id"`
	State    string `json:"state"`
	Optional bool   `json:"optional"`
	Selected bool   `json:"selected"`
	Unit     string `json:"unit,omitempty"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	// Restart says why the unit restarts: "version", "library", "first" or
	// "unknown"; empty when it stays as it is.
	Restart string `json:"restart,omitempty"`
}

// PlanView ist die Antwort von GET /api/plan.
type PlanView struct {
	BundleVersion string                    `json:"bundle_version"`
	Steps         []PlanStep                `json:"steps"`
	Components    map[string]ComponentDelta `json:"components"`
}

// RunMode unterscheidet die drei Einstiegspunkte. Sie fuehren nicht zu
// verschiedenen Codepfaden, sondern nur zu verschiedenen Schrittlisten.
type RunMode string

const (
	ModeInstall  RunMode = "install"
	ModeRedeploy RunMode = "redeploy"
	ModeRepair   RunMode = "repair"
)

// RunRequest ist der Rumpf von POST /api/run.
type RunRequest struct {
	Mode RunMode `json:"mode"`
	// Only faehrt genau einen Schritt - der Reparaturknopf der Diagnose.
	Only string `json:"only,omitempty"`
	// Secrets gehen hinein und nie wieder hinaus.
	MQTTPassword  string `json:"mqtt_password,omitempty"`
	AdminPassword string `json:"admin_password,omitempty"`
	TargetUser    string `json:"target_user,omitempty"`
	TargetBase    string `json:"target_base,omitempty"`
	// MQTTUser ist der Broker-Benutzer, den Schritt 20 anlegt.
	MQTTUser string `json:"mqtt_user,omitempty"`
	// RestartAll starts every service unit again, not only the ones whose
	// version changed (the redeploy page's "Restart all services" switch).
	RestartAll bool `json:"restart_all,omitempty"`
	// RunID ist die ID, unter der StartRun den Lauf meldet. Ein Wirt, dessen
	// Prozess den Lauf nicht ueberlebt (Plan D: Schritt 60 ersetzt das
	// Dashboard), legt sie beim Auftrag ab und nimmt ihn unter derselben ID
	// wieder auf - sonst verwirft die offene Seite dessen run-finished.
	RunID string `json:"-"`
}

// Secrets liefert die Werte, die aus jeder Ausgabe gefiltert werden muessen.
func (r RunRequest) Secrets() []string {
	return []string{r.MQTTPassword, r.AdminPassword}
}

// Check ist eine Zeile der Diagnose-Pruefliste.
type Check struct {
	Name        string `json:"name"`
	OK          bool   `json:"ok"`
	Detail      string `json:"detail"`
	RetryStepID string `json:"retry_step_id,omitempty"`
	// Group ordnet die Zeile einer Karte zu: "services", "system", "config".
	Group string `json:"group"`
	// Subject ist das Gepruefte ohne Praefix: Unit-Name, Port, Dateiname.
	Subject string `json:"subject"`
	// Severity "warn" macht aus einer fehlgeschlagenen Pruefung einen Hinweis.
	Severity string `json:"severity,omitempty"`
}

// DiagnoseView ist die Antwort von GET /api/diagnose.
type DiagnoseView struct {
	BundleVersion string            `json:"bundle_version"`
	Units         map[string]string `json:"units"`
	Ports         map[string]bool   `json:"ports"`
	Checks        []Check           `json:"checks"`
}

// Sink nimmt entgegen, was waehrend eines Laufs passiert. Der Server baut
// daraus den Ereignisstrom; ein Backend ruft nur diese Methoden auf und
// weiss nichts von SSE.
type Sink interface {
	// Marker meldet einen ##STEP-Marker. state ist "begin", "ok", "skip" oder
	// "fail"; detail traegt den Grund bzw. den Fehlercode.
	Marker(stepID, state, detail string)
	// Log meldet eine Zeile Menschentext eines Schritts.
	Log(stepID, line string)
	// Message meldet eine uebersetzte Nachricht eines Schritts: einen Schluessel
	// aus dem Katalog plus ihre Platzhalter.
	Message(stepID, key string, args map[string]string)
}

// Backend ist die einzige Naht zwischen Schicht 2 und dem Wirt.
type Backend interface {
	Describe() Description
	Connect(ctx context.Context, req ConnectRequest) (ConnectResult, error)
	GenerateKeypair(ctx context.Context) (KeypairResult, error)
	Precheck(ctx context.Context) (*Precheck, error)
	Manifest(ctx context.Context) (*ManifestView, error)
	Selection(ctx context.Context) (*SelectionView, error)
	SaveSelection(ctx context.Context, steps map[string]bool) error
	Plan(ctx context.Context) (*PlanView, error)
	Run(ctx context.Context, req RunRequest, sink Sink) error
	Diagnose(ctx context.Context) (*DiagnoseView, error)
}

// Error ist ein Backend-Fehler mit sprachneutralem Code. Der Server bildet ihn
// auf {"error":…,"detail":…} ab; alles andere wird BACKEND_ERROR.
type Error struct {
	Code   string
	Detail string
	Status int
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return e.Code
	}
	return e.Code + ": " + e.Detail
}
