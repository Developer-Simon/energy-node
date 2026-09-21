// Command fakehost serviert die Oberflaeche gegen das Attrappen-Backend mit
// den Daten der Vorlagen. Zwei Zwecke: die Browser-Tests aus Plan C-II und
// die Arbeit an einem Bildschirm ohne Raspberry Pi in Reichweite.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	webui "github.com/Developer-Simon/energy-node-webui"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
	"github.com/Developer-Simon/energy-node-webui/i18n"
	"github.com/Developer-Simon/energy-node-webui/hostapi/hostapitest"
)

// debugHandler wraps the main HTTP handler and adds a debug endpoint.
func debugHandler(mainHandler http.Handler, backend *hostapitest.FakeBackend) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/debug/state" && r.Method == http.MethodGet {
			state := struct {
				UploadedName string `json:"uploaded_name"`
			}{
				UploadedName: backend.UploadedName,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(state)
			return
		}
		mainHandler.ServeHTTP(w, r)
	})
}

func main() {
	port := flag.Int("port", 8099, "Port auf 127.0.0.1")
	language := flag.String("lang", "de", "Sprache beim ersten Laden")
	scenario := flag.String("scenario", "vorlage", "vorlage (Erstinstallation) oder vorlage-update")
	dashboard := flag.Bool("dashboard", false, "als Dashboard-Wirt auftreten: kein Verbindungsbildschirm, feste Sprache")
	trusted := flag.Bool("trusted", false, "Host-Schluessel gilt als bekannt: kein Fingerabdruck-Dialog")
	hold := flag.String("hold-step", "", "an diesem Schritt bis zum Abbrechen warten")
	failStep := flag.String("fail-step", "", "diesen Schritt fehlschlagen lassen, Format <id>:<CODE>")
	delay := flag.Duration("step-delay", 150*time.Millisecond, "Pause je Schritt")
	flag.Parse()

	catalogs, err := i18n.Load(webui.Catalogs())
	if err != nil {
		log.Fatal(err)
	}
	opts := options{dashboard: *dashboard, trusted: *trusted, holdStep: *hold, stepDelay: *delay}
	if *failStep != "" {
		opts.failStep, opts.failCode = splitFailSpec(*failStep)
	}
	stagedBackend := newScenario(*scenario, opts)
	server, err := hostapi.New(hostapi.Options{
		Backend:       stagedBackend,
		Catalogs:      catalogs,
		Language:      *language,
		LanguageFixed: *dashboard,
	})
	if err != nil {
		log.Fatal(err)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	fmt.Printf("http://%s/\n", addr)
	handler := debugHandler(server.Handler(), stagedBackend.FakeBackend)
	log.Fatal(http.ListenAndServe(addr, handler))
}

// splitFailSpec zerlegt "50:PIP_EXTERNALLY_MANAGED".
func splitFailSpec(spec string) (string, string) {
	id, code, found := strings.Cut(spec, ":")
	if !found || code == "" {
		return id, "UNIT_START_FAILED"
	}
	return id, code
}
