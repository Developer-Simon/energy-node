// Package webui buendelt die Oberflaeche des Installers als eigenes Go-Modul:
// Templates, statische Assets und die Nachrichtenkataloge. Es existiert als
// drittes Modul, weil go:embed nicht ueber Modulgrenzen reicht und sowohl
// installer/ als auch dashboard/ dieselben Bytes einbetten muessen.
//
// Das Paket kennt weder SSH noch das Dateisystem des Node. Es liefert Bytes
// und eine Version - alles Weitere ist hostapi (Schicht 2) und der Wirt.
package webui

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"
	"sync"
)

//go:embed VERSION
var versionFile string

//go:embed templates/*.html catalogs/*.json static
var files embed.FS

var (
	assetOnce    sync.Once
	assetVersion string
)

// AssetVersion ist die zentrale Cache-Bust-Marke aller JS- und CSS-URLs: die
// Version aus webui/VERSION, die die CI bei jedem PR patch-bumpt, und dazu ein
// kurzer Hash ueber die eingebetteten Assets. Der Hash bustet auch lokale
// Builds ohne neue VERSION, sonst haelt der Browser alte Skripte einen Tag lang
// neben einer frischen Vorlage. Handgepflegte ?v=N je Datei gibt es nicht.
func AssetVersion() string {
	assetOnce.Do(func() {
		h := sha256.New()
		_ = fs.WalkDir(files, "static", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := files.ReadFile(path)
			if err != nil {
				return err
			}
			h.Write([]byte(path))
			h.Write([]byte{0})
			h.Write(data)
			return nil
		})
		assetVersion = strings.TrimSpace(versionFile) + "-" + hex.EncodeToString(h.Sum(nil))[:8]
	})
	return assetVersion
}

// Templates liefert die eingebetteten HTML-Vorlagen als eigenes FS-Wurzelwerk,
// damit ein Wirt sie mit template.ParseFS(webui.Templates(), "*.html") laedt.
func Templates() fs.FS {
	sub, err := fs.Sub(files, "templates")
	if err != nil {
		panic("webui: templates/ fehlt im eingebetteten FS: " + err.Error())
	}
	return sub
}

// Catalogs liefert die Nachrichtenkataloge (de.json, en.json, spaeter mehr).
func Catalogs() fs.FS {
	sub, err := fs.Sub(files, "catalogs")
	if err != nil {
		panic("webui: catalogs/ fehlt im eingebetteten FS: " + err.Error())
	}
	return sub
}

// StaticHandler bedient /assets/ aus dem eingebetteten static/-Baum. prefix
// ist der Pfadanteil, der vor dem Dateinamen steht und abgeschnitten wird.
// Die Cache-Vorgabe ist dieselbe wie im Dashboard: ein Tag, ohne immutable -
// die Frische regelt die ?v=-Marke, nicht der Header.
func StaticHandler(prefix string) http.Handler {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic("webui: static/ fehlt im eingebetteten FS: " + err.Error())
	}
	server := http.StripPrefix(prefix, http.FileServer(http.FS(sub)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		server.ServeHTTP(w, r)
	})
}
