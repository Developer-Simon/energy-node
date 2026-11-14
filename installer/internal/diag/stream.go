package diag

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/Developer-Simon/energy-node-installer/internal/steps"
	"github.com/Developer-Simon/energy-node-installer/internal/transport"
)

// Sections lists the section markers of diagnose.sh in the order it prints
// them. Der Abschnitt apt laeuft im Hintergrund und kommt als letzter.
var Sections = []string{"bundle", "units", "ports", "mqtt", "webhook", "config", "info", "tailscale", "reboot", "apt"}

// Assembler baut den Bericht aus den Zeilen von diagnose.sh --stream auf.
// Die Zuordnung folgt dem Python-Skript im Bash-Skript, nur dass eine
// kaputte Zeile hier keinen Abbruch ausloest, sondern den Wert leer laesst.
type Assembler struct {
	report Report
	seen   map[string]bool
}

// NewAssembler liefert einen leeren Bericht mit denselben Vorgaben wie das
// Python-Skript (leere Maps und Listen, nil fuer apt und webhook).
func NewAssembler() *Assembler {
	return &Assembler{
		report: Report{
			Steps: map[string]string{},
			Units: map[string]string{},
			Ports: map[string]bool{},
			Config: ConfigReport{
				Manifests: []string{},
			},
			Versions: VersionsReport{
				Components: map[string]string{},
				Services:   map[string]string{},
			},
			Devices: map[string][]Device{},
		},
		seen: map[string]bool{},
	}
}

// Line wendet eine Zeile "art<TAB>schluessel<TAB>wert" an. Gibt den
// Abschnittsnamen zurueck, wenn die Zeile eine Marke war, sonst "".
func (a *Assembler) Line(line string) (section string) {
	parts := strings.Split(strings.TrimRight(line, "\n"), "\t")
	if len(parts) < 2 {
		return ""
	}
	kind, key := parts[0], parts[1]
	value := ""
	if len(parts) > 2 {
		value = parts[2]
	}
	r := &a.report

	switch kind {
	case "section":
		if value == "done" && slices.Contains(Sections, key) {
			a.seen[key] = true
			return key
		}
	case "bundle":
		r.BundleVersion = value
	case "step":
		r.Steps[key] = value
	case "unit":
		if value == "" {
			value = "unbekannt"
		}
		r.Units[key] = value
	case "port":
		r.Ports[key] = value == "true"
	case "config":
		if key == "config.json" {
			r.Config.ConfigJSON = value == "true"
		}
	case "manifest":
		r.Config.Manifests = append(r.Config.Manifests, key)
	case "tailscale":
		if key == "angemeldet" {
			r.Tailscale.Angemeldet = value == "true"
		}
	case "mqtt":
		if key == "login" {
			r.MQTTLogin = value
		}
	case "version":
		group, name, _ := strings.Cut(key, ":")
		if group == "component" {
			r.Versions.Components[name] = value
		} else {
			r.Versions.Services[name] = value
		}
	case "devices":
		var devices []Device
		if err := json.Unmarshal([]byte(value), &devices); err != nil {
			devices = nil
		}
		r.Devices[key] = devices
	case "apt":
		var updates *steps.SystemUpdates
		if err := json.Unmarshal([]byte(value), &updates); err != nil {
			updates = nil
		}
		r.SystemUpdates = updates
	case "reboot":
		r.RebootRequired = value == "true"
	case "webhook":
		if r.ShellyWebhook == nil {
			r.ShellyWebhook = &ShellyWebhookReport{}
		}
		switch key {
		case "port":
			r.ShellyWebhook.Port = value
		case "firewall":
			r.ShellyWebhook.Firewall = value == "true"
		case "listening":
			r.ShellyWebhook.Listening = value == "true"
		}
	}
	return ""
}

// Report liefert eine tiefe Kopie des bisherigen Berichts. Config.Manifests
// ist sortiert, wie im Python-Skript. nil bleibt nil, leer bleibt leer.
func (a *Assembler) Report() *Report {
	src := &a.report
	out := &Report{
		BundleVersion: src.BundleVersion,
		Steps:         maps.Clone(src.Steps),
		Units:         maps.Clone(src.Units),
		Ports:         maps.Clone(src.Ports),
		Config: ConfigReport{
			ConfigJSON: src.Config.ConfigJSON,
			Manifests:  cloneStrings(src.Config.Manifests),
		},
		Tailscale: src.Tailscale,
		Versions: VersionsReport{
			Components: maps.Clone(src.Versions.Components),
			Services:   maps.Clone(src.Versions.Services),
		},
		Devices:        make(map[string][]Device, len(src.Devices)),
		RebootRequired: src.RebootRequired,
		SystemUpdates:  cloneSystemUpdates(src.SystemUpdates),
		MQTTLogin:      src.MQTTLogin,
	}
	slices.Sort(out.Config.Manifests)
	for name, devices := range src.Devices {
		out.Devices[name] = cloneDevices(devices)
	}
	if src.ShellyWebhook != nil {
		hook := *src.ShellyWebhook
		out.ShellyWebhook = &hook
	}
	return out
}

// Pending liefert die noch offenen Abschnitte in der Reihenfolge von Sections.
func (a *Assembler) Pending() []string {
	pending := make([]string, 0, len(Sections))
	for _, section := range Sections {
		if !a.seen[section] {
			pending = append(pending, section)
		}
	}
	return pending
}

// cloneStrings kopiert eine Liste und behaelt dabei nil und leer getrennt.
func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// cloneDevices kopiert eine Geraeteliste und behaelt nil (unlesbare
// Geraetedatei) getrennt von der leeren Liste.
func cloneDevices(in []Device) []Device {
	if in == nil {
		return nil
	}
	out := make([]Device, len(in))
	copy(out, in)
	return out
}

// cloneSystemUpdates kopiert die ausstehenden Pakete, nil bleibt nil.
func cloneSystemUpdates(in *steps.SystemUpdates) *steps.SystemUpdates {
	if in == nil {
		return nil
	}
	out := *in
	if in.Packages != nil {
		out.Packages = make([]steps.SystemPackage, len(in.Packages))
		copy(out.Packages, in.Packages)
	}
	return &out
}

// RunStream startet diagnose.sh --stream auf dem Node und meldet nach jeder
// Abschnittsmarke synchron onSection mit dem bisherigen Bericht und den noch
// offenen Abschnitten. Endet der Befehl, ohne dass alle Abschnitte da sind,
// gibt es einen Fehler mit stderr, wie bei Run.
func RunStream(ctx context.Context, client *transport.Client, remoteBundleDir, remoteStateDir, bundleVersion string, onSection func(report *Report, pending []string)) error {
	env := map[string]string{
		"EN_STATE_DIR":      remoteStateDir,
		"EN_BUNDLE_DIR":     remoteBundleDir,
		"EN_BUNDLE_VERSION": bundleVersion,
	}
	scriptPath := path.Join(remoteBundleDir, "bootstrap", "diagnose.sh")
	command := transport.BuildCommand(env, "bash "+transport.ShellQuote(scriptPath)+" --stream")

	pipeReader, pipeWriter := io.Pipe()
	assembler := NewAssembler()
	readDone := make(chan error, 1)

	// raw sammelt die Ausgabe, solange keine Marke kam: ein Paket vor dem
	// Stream kennt --stream nicht und gibt den ganzen Bericht als JSON aus.
	var raw bytes.Buffer
	marked := false

	go func() {
		// Der Puffer ist 1 MiB groß, weil die apt-Zeile lang werden kann.
		scanner := bufio.NewScanner(pipeReader)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			if !marked {
				raw.Write(scanner.Bytes())
				raw.WriteByte('\n')
			}
			if section := assembler.Line(scanner.Text()); section != "" {
				marked = true
				raw.Reset()
				if onSection != nil {
					onSection(assembler.Report(), assembler.Pending())
				}
			}
		}
		err := scanner.Err()
		// Bricht der Leser ab, darf der Schreiber nicht ewig blockieren.
		_ = pipeReader.CloseWithError(err)
		readDone <- err
	}()

	var stderr bytes.Buffer
	runErr := client.Run(ctx, command, pipeWriter, &stderr)
	_ = pipeWriter.Close()
	scanErr := <-readDone

	if runErr != nil {
		return fmt.Errorf("running diagnose.sh --stream: %w (stderr: %s)", runErr, stderr.String())
	}
	if scanErr != nil {
		return fmt.Errorf("reading diagnose.sh --stream output: %w", scanErr)
	}
	// Ein Paket vor dem Stream ignoriert --stream und gibt den Bericht als
	// JSON aus. Er kommt dann als ein fertiger Stand.
	if !marked {
		report, err := parseReport(raw.Bytes())
		if err != nil {
			return fmt.Errorf("diagnose.sh --stream gave neither stream lines nor a JSON report: %w (stdout: %q, stderr: %s)", err, raw.String(), stderr.String())
		}
		if onSection != nil {
			onSection(report, []string{})
		}
		return nil
	}
	if pending := assembler.Pending(); len(pending) > 0 {
		return fmt.Errorf("diagnose.sh --stream ended before all sections were reported (missing %s, stderr: %s)", strings.Join(pending, ", "), stderr.String())
	}
	return nil
}
