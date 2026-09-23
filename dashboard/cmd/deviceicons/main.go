// Command deviceicons writes the device icon catalogue from
// internal/webui/deviceicons.go to integrations/homeassistant/icons.source.json,
// the input of the Home Assistant icon module. The dashboard itself does not
// read that file; it exists so scripts/icons/flatten_icons.py never has to
// parse Go. Run
//
//	(cd dashboard && go run ./cmd/deviceicons)
//
// after changing a drawing, then regenerate the module. "-check" exits 1 on
// drift; TestCommittedCatalogueIsCurrent runs the same check in CI.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Developer-Simon/energy-node-dashboard/internal/webui"
)

type icon struct {
	Name   string `json:"name"`
	HAName string `json:"ha_name"`
	Label  string `json:"label"`
	Markup string `json:"markup"`
	SHA256 string `json:"sha256"`
}

type catalogueFile struct {
	GeneratedBy string `json:"generated_by"`
	StrokeWidth string `json:"stroke_width"`
	ViewBox     string `json:"view_box"`
	Icons       []icon `json:"icons"`
}

func main() {
	check := flag.Bool("check", false, "exit 1 if the committed catalogue differs from the freshly generated one")
	flag.Parse()

	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "deviceicons:", err)
		os.Exit(1)
	}
	generated, err := render()
	if err != nil {
		fmt.Fprintln(os.Stderr, "deviceicons:", err)
		os.Exit(1)
	}
	target := targetPath(root)

	if *check {
		current, err := os.ReadFile(target)
		if err != nil {
			fmt.Fprintln(os.Stderr, "deviceicons:", err)
			os.Exit(1)
		}
		if !bytes.Equal(current, generated) {
			fmt.Fprintf(os.Stderr, "deviceicons: %s is stale — run: (cd dashboard && go run ./cmd/deviceicons)\n", target)
			os.Exit(1)
		}
		return
	}

	if err := os.WriteFile(target, generated, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "deviceicons:", err)
		os.Exit(1)
	}
	fmt.Println("wrote", target)
}

func render() ([]byte, error) {
	catalogue := webui.DeviceIconCatalogue()
	icons := make([]icon, len(catalogue))
	for i, dev := range catalogue {
		markup := string(dev.Markup)
		hash := sha256.Sum256([]byte(markup))
		icons[i] = icon{
			Name:   dev.Name,
			HAName: strings.TrimPrefix(dev.Name, "mdi:"),
			Label:  dev.Label,
			Markup: markup,
			SHA256: fmt.Sprintf("%x", hash),
		}
	}

	doc := catalogueFile{
		GeneratedBy: "dashboard/cmd/deviceicons/main.go",
		StrokeWidth: webui.DeviceIconStrokeWidth,
		ViewBox:     "0 0 24 24",
		Icons:       icons,
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// repoRoot walks up from the working directory until it finds a directory that
// holds both "dashboard" and "services" — the repository root. This makes the
// tool work from "go run ./cmd/deviceicons" (cwd = dashboard/) as well as from
// "go test" (cwd = dashboard/cmd/deviceicons/).
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if isDir(filepath.Join(dir, "dashboard")) && isDir(filepath.Join(dir, "services")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("repository root (a dir with dashboard/ and services/) not found above the working directory")
		}
		dir = parent
	}
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func targetPath(root string) string {
	return filepath.Join(root, "integrations", "homeassistant", "icons.source.json")
}
