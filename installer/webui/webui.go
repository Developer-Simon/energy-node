// Package webui buendelt die Oberflaeche des Installers als eigenes Go-Modul:
// Templates, statische Assets und die Nachrichtenkataloge. Es existiert als
// drittes Modul, weil go:embed nicht ueber Modulgrenzen reicht und sowohl
// installer/ als auch dashboard/ dieselben Bytes einbetten muessen.
//
// Das Paket kennt weder SSH noch das Dateisystem des Node. Es liefert Bytes
// und eine Version - alles Weitere ist hostapi (Schicht 2) und der Wirt.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed VERSION
var versionFile string

//go:embed templates/*.html catalogs/*.json static
var files embed.FS

// AssetVersion ist die zentrale Cache-Bust-Marke aller JS- und CSS-URLs. Sie
// kommt aus webui/VERSION, das die CI bei jedem PR patch-bumpt, der das Modul
// anfasst - jede Asset-Aenderung bustet damit von selbst. Handgepflegte ?v=N
// je Datei gibt es in diesem Modul deshalb nicht.
func AssetVersion() string {
	return strings.TrimSpace(versionFile)
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
