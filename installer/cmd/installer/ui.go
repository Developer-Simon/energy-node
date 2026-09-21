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
	"runtime"
	"syscall"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
	"github.com/Developer-Simon/energy-node-installer/internal/bundlesource"
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

type stateLayout struct{ work, cache string }

// stateDirs legt fest, wo der Installer Zwischenstaende ablegt: entpackte
// Pakete unter work (werden aufgeraeumt), heruntergeladene und gebaute
// Archive unter cache (bleiben, damit ein zweiter Lauf nichts erneut laedt).
func stateDirs(home string) stateLayout {
	base := filepath.Join(home, ".energy-node")
	return stateLayout{work: filepath.Join(base, "work"), cache: filepath.Join(base, "cache")}
}

func newUIConfig(args []string) (uiConfig, error) {
	fs := flag.NewFlagSet("installer", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	port := fs.Int("port", 0, "Port auf 127.0.0.1; 0 laesst das Betriebssystem waehlen")
	bundleDir := fs.String("bundle", "", "entpacktes Bundle (optional); leer heisst: neben dem Programm suchen")
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

	dirs := stateDirs(stateHome)
	publicKey, err := bundle.EmbeddedPublicKey()
	if err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	exeDir := filepath.Dir(bundleDir)
	backend, err := host.New(host.Config{
		BundleDir:      bundleDir,
		KnownHostsPath: filepath.Join(stateHome, ".energy-node", "known_hosts"),
		IdentityDir:    filepath.Join(stateHome, ".energy-node"),
		RepoPath:       bundlesource.DetectRepo(cwd, exeDir),
		Resolver: &bundlesource.Resolver{
			BundledDir: bundleDir,
			WorkDir:    dirs.work,
			GitHub:     &bundlesource.GitHub{CacheDir: dirs.cache},
			PublicKey:  publicKey,
		},
	})
	if err != nil {
		return err
	}
	defer backend.Close()

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
		// E6's process-lifetime rule: the WebView's event loop must own the
		// process's original OS thread, so this must run on the goroutine
		// main() called us on, not a spawned one.
		runtime.LockOSThread()
		mode, err := shell.Open(url)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Kein Fenster: %v\nDie Oberflaeche ist trotzdem unter der Adresse oben erreichbar.\n", err)
		} else if note, waitForSignal := describeShellMode(mode); !waitForSignal {
			// ModeWebview: shell.Open already blocked until the window
			// closed. There is nothing left to wait for.
			return httpServer.Shutdown(context.Background())
		} else if note != "" {
			fmt.Fprintln(os.Stderr, note)
		}
	}

	// Only registered after shell.Open: for ModeWebview Open blocks until the
	// window closes, and nothing reads stop during that time - registering
	// earlier would swallow Ctrl+C instead of letting the default handler
	// end the process.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errs:
		return err
	case <-stop:
		return httpServer.Shutdown(context.Background())
	}
}

// describeShellMode maps a shell.Mode to the operator-facing note (if any)
// and whether runUI should still wait for SIGINT afterward. ModeWebview is
// the only mode that answers false: shell.Open already blocked until the
// window closed.
func describeShellMode(mode shell.Mode) (note string, waitForSignal bool) {
	switch mode {
	case shell.ModeWebview:
		return "", false
	case shell.ModeBrowser:
		return "Kein eigenes Fenster gefunden - die Oberflaeche laeuft im Standardbrowser.", true
	case shell.ModeURLOnly:
		return "Kein Fenster und kein Browser gefunden - die Adresse steht oben.", true
	default: // shell.ModeApp
		return "", true
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
