package diag

import (
	"context"
	"path"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-installer/internal/transport"
	"github.com/Developer-Simon/energy-node-installer/internal/transport/transporttest"
)

// Die Rohzeilen bilden dieselben Werte ab wie streamJSONReport.
var streamRawLines = []string{
	"bundle\tversion\tv0.2.0",
	"step\t10\tv0.2.0",
	"section\tbundle\tdone",
	"unit\tmosquitto.service\tactive",
	"unit\tcaddy.service\tinactive",
	"unit\tempty.service\t",
	"section\tunits\tdone",
	"port\t1883\ttrue",
	"port\t8080\tfalse",
	"section\tports\tdone",
	"mqtt\tlogin\tdenied",
	"section\tmqtt\tdone",
	"webhook\tport\t8443",
	"webhook\tfirewall\ttrue",
	"webhook\tlistening\tfalse",
	"section\twebhook\tdone",
	"config\tconfig.json\ttrue",
	"manifest\ttuya\t",
	"manifest\tshelly\t",
	"section\tconfig\tdone",
	"version\tcomponent:bootstrap\tv0.1.10",
	"version\tservice:shelly-rpc.service\tv0.4.2",
	"devices\tshelly-rpc.service\t[{\"id\": \"plug\", \"name\": \"Plug\"}]",
	"devices\ttuya.service\tnull",
	"section\tinfo\tdone",
	"tailscale\tangemeldet\ttrue",
	"section\ttailscale\tdone",
	"reboot\trequired\ttrue",
	"section\treboot\tdone",
	"apt\tpending\t{\"count\": 1, \"checked_at\": \"\", \"packages\": [{\"name\": \"openssl\", \"from\": \"3.0.11\", \"to\": \"3.0.13\"}]}",
	"section\tapt\tdone",
}

// streamJSONReport ist, was der Python-Baukasten fuer dieselben Zeilen als
// JSON ausgibt.
const streamJSONReport = `{
  "bundle_version": "v0.2.0",
  "steps": {"10": "v0.2.0"},
  "units": {"mosquitto.service": "active", "caddy.service": "inactive", "empty.service": "unbekannt"},
  "ports": {"1883": true, "8080": false},
  "mqtt": {"login": "denied"},
  "shelly_webhook": {"port": "8443", "firewall": true, "listening": false},
  "config": {"config.json": true, "manifests": ["shelly", "tuya"]},
  "tailscale": {"angemeldet": true},
  "reboot_required": true,
  "system_updates": {"count": 1, "checked_at": "", "packages": [{"name": "openssl", "from": "3.0.11", "to": "3.0.13"}]},
  "versions": {"components": {"bootstrap": "v0.1.10"}, "services": {"shelly-rpc.service": "v0.4.2"}},
  "devices": {"shelly-rpc.service": [{"id": "plug", "name": "Plug"}], "tuya.service": null}
}`

func TestAssemblerMirrorsTheJSONBuilder(t *testing.T) {
	assembler := NewAssembler()
	for _, line := range streamRawLines {
		assembler.Line(line)
	}
	if pending := assembler.Pending(); len(pending) != 0 {
		t.Fatalf("all sections were streamed, pending should be empty, got %v", pending)
	}

	want, err := parseReport([]byte(streamJSONReport))
	if err != nil {
		t.Fatalf("parseReport: %v", err)
	}
	got := assembler.Report()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("assembled report differs from the JSON builder:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestAssemblerReportsSectionsAndPending(t *testing.T) {
	assembler := NewAssembler()
	if pending := assembler.Pending(); !slices.Equal(pending, Sections) {
		t.Fatalf("before any marker everything is pending, got %v", pending)
	}

	if section := assembler.Line("section\tunits\tdone"); section != "units" {
		t.Fatalf("marker should return its section, got %q", section)
	}
	want := slices.DeleteFunc(slices.Clone(Sections), func(s string) bool { return s == "units" })
	if pending := assembler.Pending(); !slices.Equal(pending, want) {
		t.Errorf("pending after units = %v, want %v", pending, want)
	}

	if section := assembler.Line("unit\tmosquitto.service\tactive"); section != "" {
		t.Errorf("a data line must return an empty section, got %q", section)
	}
}

func TestAssemblerSurvivesBrokenLines(t *testing.T) {
	assembler := NewAssembler()
	assembler.Line("devices\tx.service\t{kaputt")
	assembler.Line("apt\tpending\t{kaputt")
	if devices, ok := assembler.Report().Devices["x.service"]; !ok || devices != nil {
		t.Errorf("a broken device line must leave a nil list, got %+v (present %v)", devices, ok)
	}
	if updates := assembler.Report().SystemUpdates; updates != nil {
		t.Errorf("a broken apt line must leave SystemUpdates nil, got %+v", updates)
	}

	fresh := NewAssembler().Report()
	for _, line := range []string{"foo\tbar\tbaz", "nur-ein-feld", ""} {
		if section := assembler.Line(line); section != "" {
			t.Errorf("line %q must not be a marker, got %q", line, section)
		}
	}
	// Die Zeilen oben haben die bisherigen Devices- und Apt-Werte nicht
	// veraendert: nur das fremde Feld fehlt im Vergleich zum leeren Bericht.
	got := assembler.Report()
	got.Devices = fresh.Devices
	got.SystemUpdates = fresh.SystemUpdates
	if !reflect.DeepEqual(got, fresh) {
		t.Errorf("unknown and empty lines must not change the report:\n got: %+v\nwant: %+v", got, fresh)
	}
}

func TestAssemblerReportIsADeepCopy(t *testing.T) {
	assembler := NewAssembler()
	assembler.Line("manifest\ttuya\t")
	assembler.Line("manifest\tshelly\t")
	assembler.Line("devices\tshelly-rpc.service\t[{\"id\": \"plug\", \"name\": \"Plug\"}]")
	first := assembler.Report()
	if !slices.Equal(first.Config.Manifests, []string{"shelly", "tuya"}) {
		t.Fatalf("manifests must be sorted in the report, got %v", first.Config.Manifests)
	}

	first.Config.Manifests[0] = "changed"
	first.Devices["shelly-rpc.service"][0].Name = "changed"
	assembler.Line("manifest\tzzz\t")

	second := assembler.Report()
	if second.Config.Manifests[0] != "shelly" || second.Devices["shelly-rpc.service"][0].Name != "Plug" {
		t.Errorf("a returned report must not alias the assembler state: %+v", second)
	}
}

// streamSFTPServer ueberspringt den Test ohne sftp-server, wie report_test.go.
func streamSFTPServer(t *testing.T) {
	t.Helper()
	if _, ok := transporttest.SFTPServerPath(); !ok {
		t.Skip("no sftp-server binary found; install openssh-server to run this test")
	}
}

func dialStreamTest(t *testing.T, sshd *transporttest.SSHD) *transport.Client {
	t.Helper()
	store := transport.NewHostKeyStore(path.Join(t.TempDir(), "known_hosts"))
	callback, err := store.Callback(func(string, string) (bool, error) { return true, nil })
	if err != nil {
		t.Fatalf("Callback: %v", err)
	}
	client, err := transport.Dial(context.Background(), transport.Config{
		Host:            sshd.Addr,
		User:            sshd.User(),
		PrivateKeyPEM:   sshd.ClientKeyPEM,
		HostKeyCallback: callback,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// uploadStreamScript legt ein Fake-diagnose.sh unter bundle/bootstrap ab und
// liefert das Bundle-Verzeichnis.
func uploadStreamScript(t *testing.T, client *transport.Client, script string) string {
	t.Helper()
	bundleDir := t.TempDir()
	scriptPath := path.Join(bundleDir, "bootstrap", "diagnose.sh")
	if err := client.UploadBytes([]byte(script), scriptPath, 0o755); err != nil {
		t.Fatalf("uploading fake diagnose.sh: %v", err)
	}
	return bundleDir
}

const fakeStreamScript = `#!/bin/sh
printf 'bundle\tversion\t%s\n' "$EN_BUNDLE_VERSION"
printf 'section\tbundle\tdone\n'
sleep 0.2
printf 'unit\tmosquitto.service\tactive\n'
printf 'section\tunits\tdone\n'
sleep 0.2
for section in ports mqtt webhook config info tailscale reboot apt; do
  printf 'section\t%s\tdone\n' "$section"
done
`

func TestRunStreamReportsEachSection(t *testing.T) {
	streamSFTPServer(t)
	sshd := transporttest.Start(t)
	client := dialStreamTest(t, sshd)
	bundleDir := uploadStreamScript(t, client, fakeStreamScript)

	var calls []int
	var firstCall time.Time
	onSection := func(report *Report, pending []string) {
		if len(calls) == 0 {
			firstCall = time.Now()
		}
		calls = append(calls, len(pending))
		if len(calls) == 1 && report.BundleVersion != "v0.2.0" {
			t.Errorf("the first call must already carry the bundle version, got %q", report.BundleVersion)
		}
	}

	err := RunStream(context.Background(), client, bundleDir, "/var/lib/energy-node-installer", "v0.2.0", onSection)
	returned := time.Now()
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}

	if len(calls) != len(Sections) {
		t.Fatalf("onSection must be called once per section marker, got %d calls", len(calls))
	}
	for i := 1; i < len(calls); i++ {
		if calls[i] >= calls[i-1] {
			t.Errorf("pending must shrink with every marker, got %v", calls)
			break
		}
	}
	if calls[len(calls)-1] != 0 {
		t.Errorf("the last call must have nothing pending, got %d", calls[len(calls)-1])
	}
	if gap := returned.Sub(firstCall); gap < 300*time.Millisecond {
		t.Errorf("the first call must come before the script ends, gap was only %v", gap)
	}
}

const fakeCutStreamScript = `#!/bin/sh
printf 'bundle\tversion\t%s\n' "$EN_BUNDLE_VERSION"
printf 'section\tbundle\tdone\n'
`

func TestRunStreamFailsWhenTheStreamIsCut(t *testing.T) {
	streamSFTPServer(t)
	sshd := transporttest.Start(t)
	client := dialStreamTest(t, sshd)
	bundleDir := uploadStreamScript(t, client, fakeCutStreamScript)

	calls := 0
	err := RunStream(context.Background(), client, bundleDir, "/var/lib/energy-node-installer", "v0.2.0", func(*Report, []string) {
		calls++
	})
	if err == nil {
		t.Fatalf("a stream that ends after bundle must be an error")
	}
	if calls != 1 {
		t.Errorf("onSection must have been called once, got %d", calls)
	}
}
