package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Developer-Simon/energy-node-installer/internal/host"
	"github.com/Developer-Simon/energy-node-installer/internal/shell"
	webui "github.com/Developer-Simon/energy-node-webui"
	"github.com/Developer-Simon/energy-node-webui/hostapi"
	"github.com/Developer-Simon/energy-node-webui/i18n"
)

type uiConfig struct {
	addr       string
	bundleDir  string
	language   string
	openWindow bool
}

func newUIConfig(args []string) (uiConfig, error) {
	fs := flag.NewFlagSet("installer", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	port := fs.Int("port", 0, "Port auf 127.0.0.1; 0 laesst das Betriebssystem waehlen")
	bundleDir := fs.String("bundle", "", "entpacktes Bundle; leer heisst: neben dem Programm suchen")
	language := fs.String("lang", "", "Sprache der Oberflaeche; leer heisst: aus der OS-Locale")
	noWindow := fs.Bool("no-window", false, "kein Fenster oeffnen, nur die URL ausgeben")
	if err := fs.Parse(args); err != nil {
		return uiConfig{}, err
	}
	return uiConfig{
		addr:       fmt.Sprintf("127.0.0.1:%d", *port),
		bundleDir:  *bundleDir,
		language:   *language,
		openWindow: !*noWindow,
	}, nil
}

// newToken erzeugt das Einmal-Token, das nur in der geoeffneten URL steht.
func newToken() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func runUI(cfg uiConfig) error {
	catalogs, err := i18n.Load(webui.Catalogs())
	if err != nil {
		return err
	}
	language := cfg.language
	if language == "" {
		language = i18n.Preferred(i18n.OSLocale(), catalogs.Languages())
	}

	bundleDir := cfg.bundleDir
	if bundleDir == "" {
		bundleDir = defaultBundleDir()
	}
	stateHome, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	backend, err := host.New(host.Config{
		BundleDir:      bundleDir,
		KnownHostsPath: filepath.Join(stateHome, ".energy-node", "known_hosts"),
		IdentityDir:    filepath.Join(stateHome, ".energy-node"),
	})
	if err != nil {
		return err
	}

	token, err := newToken()
	if err != nil {
		return err
	}
	server, err := hostapi.New(hostapi.Options{
		Backend: backend, Catalogs: catalogs, Language: language, Token: token,
	})
	if err != nil {
		return err
	}

	// Erst lauschen, dann die URL bilden: der Port steht damit fest, bevor
	// irgendjemand ihn benutzt.
	listener, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s/?token=%s", listener.Addr().String(), token)

	httpServer := &http.Server{Handler: server.Handler()}
	errs := make(chan error, 1)
	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()

	fmt.Println(url)
	if cfg.openWindow {
		mode, err := shell.Open(url)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Kein Fenster: %v\nDie Oberflaeche ist trotzdem unter der Adresse oben erreichbar.\n", err)
		} else if mode == "browser" {
			fmt.Fprintln(os.Stderr, "Kein Chrome/Edge gefunden - die Oberflaeche laeuft im Standardbrowser.")
		}
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errs:
		return err
	case <-stop:
		return httpServer.Shutdown(context.Background())
	}
}

// defaultBundleDir sucht das Bundle neben dem Programm - so laeuft eine
// entpackte Auslieferung ohne Schalter.
func defaultBundleDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "bundle"
	}
	return filepath.Join(filepath.Dir(exe), "bundle")
}
