package bundlesource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
)

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// pinServer serves files by path and counts every request it gets.
type pinServer struct {
	srv      *httptest.Server
	requests atomic.Int32
}

func newPinServer(t *testing.T, files map[string][]byte) *pinServer {
	t.Helper()
	ps := &pinServer{}
	ps.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ps.requests.Add(1)
		data, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(data)
	}))
	t.Cleanup(ps.srv.Close)
	return ps
}

// pinnedResolver points the host allowlist and scheme at the test server.
func pinnedResolver(t *testing.T, ps *pinServer) *Resolver {
	t.Helper()
	r := newResolver(t, nil)
	r.CacheDir = t.TempDir()
	r.rules = &packageRules{
		debHosts:   []string{"127.0.0.1"},
		wheelHosts: []string{"127.0.0.1"},
		scheme:     "http",
	}
	return r
}

// packedBundleWithPins packs an unsigned bundle whose manifest carries the
// given extra fields (packages, wheels), and optional files in wheels/.
func packedBundleWithPins(t *testing.T, arch string, extra map[string]any, wheelFiles map[string][]byte) string {
	t.Helper()
	src := t.TempDir()
	writeBundle(t, src, arch, nil)
	if len(wheelFiles) > 0 {
		for name, data := range wheelFiles {
			dir := filepath.Join(src, "wheels")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	raw, err := os.ReadFile(filepath.Join(src, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for k, v := range extra {
		m[k] = v
	}
	out, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(src, "manifest.json"), out, 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "energy-node-v9.9.9-"+arch+".tar.gz")
	if err := bundle.PackDir(src, archive); err != nil {
		t.Fatal(err)
	}
	return archive
}

func pin(file, url string, data []byte) map[string]any {
	return map[string]any{"version": "1.0", "file": file, "url": url, "sha256": sha256Hex(data), "update": "manual"}
}

func TestResolveFetchesPinnedPackages(t *testing.T) {
	caddy := []byte("caddy-deb-bytes")
	tailscale := []byte("tailscale-deb-bytes")
	ps := newPinServer(t, map[string][]byte{"/caddy.deb": caddy, "/ts.deb": tailscale})
	r := pinnedResolver(t, ps)
	archive := packedBundleWithPins(t, "armv6", map[string]any{
		"packages": map[string]any{
			"caddy":     pin("caddy.deb", ps.srv.URL+"/caddy.deb", caddy),
			"tailscale": pin("ts.deb", ps.srv.URL+"/ts.deb", tailscale),
		},
	}, nil)

	got, err := r.Resolve(context.Background(), Request{Kind: KindFile, Path: archive, Arch: "armv6"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got.Packages) != 2 || len(got.MissingPackages) != 0 {
		t.Fatalf("Packages = %+v, Missing = %v", got.Packages, got.MissingPackages)
	}
	for _, p := range got.Packages {
		data, err := os.ReadFile(p.Path)
		if err != nil {
			t.Fatalf("%s: %v", p.Name, err)
		}
		if sha256Hex(data) != p.SHA256 {
			t.Errorf("%s: checksum of the local file does not match the pin", p.Name)
		}
	}
}

func TestResolveUsesThePackageCache(t *testing.T) {
	caddy := []byte("caddy-deb-bytes")
	ps := newPinServer(t, map[string][]byte{"/caddy.deb": caddy})
	r := pinnedResolver(t, ps)
	archive := packedBundleWithPins(t, "armv6", map[string]any{
		"packages": map[string]any{"caddy": pin("caddy.deb", ps.srv.URL+"/caddy.deb", caddy)},
	}, nil)

	if _, err := r.Resolve(context.Background(), Request{Kind: KindFile, Path: archive, Arch: "armv6"}); err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	before := ps.requests.Load()
	got, err := r.Resolve(context.Background(), Request{Kind: KindFile, Path: archive, Arch: "armv6"})
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if ps.requests.Load() != before {
		t.Errorf("the second run made %d HTTP requests, want none", ps.requests.Load()-before)
	}
	if len(got.Packages) != 1 {
		t.Errorf("Packages = %+v, want the cached file", got.Packages)
	}
}

func TestResolveRejectsAPackageWithAWrongChecksum(t *testing.T) {
	ps := newPinServer(t, map[string][]byte{"/caddy.deb": []byte("tampered")})
	r := pinnedResolver(t, ps)
	archive := packedBundleWithPins(t, "armv6", map[string]any{
		"packages": map[string]any{"caddy": pin("caddy.deb", ps.srv.URL+"/caddy.deb", []byte("original"))},
	}, nil)

	_, err := r.Resolve(context.Background(), Request{Kind: KindFile, Path: archive, Arch: "armv6"})
	if code(err) != CodePackageDownloadInvalid {
		t.Fatalf("err = %v, want %s", err, CodePackageDownloadInvalid)
	}
	cached := filepath.Join(r.CacheDir, "packages", sha256Hex([]byte("original")), "caddy.deb")
	if _, statErr := os.Stat(cached); !os.IsNotExist(statErr) {
		t.Errorf("a cache file with the wrong checksum must be deleted (stat err = %v)", statErr)
	}
}

func TestResolveRejectsAPackageFromAnUnknownHost(t *testing.T) {
	r := newResolver(t, nil)
	r.CacheDir = t.TempDir()
	archive := packedBundleWithPins(t, "armv6", map[string]any{
		"packages": map[string]any{"caddy": map[string]any{
			"version": "1.0", "file": "caddy.deb", "url": "https://evil.example/caddy.deb",
			"sha256": sha256Hex([]byte("x")), "update": "manual",
		}},
	}, nil)

	_, err := r.Resolve(context.Background(), Request{Kind: KindFile, Path: archive, Arch: "armv6"})
	if code(err) != CodePackageURLRejected {
		t.Fatalf("err = %v, want %s", err, CodePackageURLRejected)
	}
}

func TestResolveRejectsAPlainHTTPPackageURL(t *testing.T) {
	r := newResolver(t, nil)
	r.CacheDir = t.TempDir()
	archive := packedBundleWithPins(t, "armv6", map[string]any{
		"packages": map[string]any{"caddy": map[string]any{
			"version": "1.0", "file": "caddy.deb", "url": "http://github.com/caddy.deb",
			"sha256": sha256Hex([]byte("x")), "update": "manual",
		}},
	}, nil)

	_, err := r.Resolve(context.Background(), Request{Kind: KindFile, Path: archive, Arch: "armv6"})
	if code(err) != CodePackageURLRejected {
		t.Fatalf("err = %v, want %s", err, CodePackageURLRejected)
	}
}

func TestResolveRecordsAnUnreachablePackageAsMissing(t *testing.T) {
	ps := newPinServer(t, map[string][]byte{})
	r := pinnedResolver(t, ps)
	archive := packedBundleWithPins(t, "armv6", map[string]any{
		"packages": map[string]any{"caddy": pin("caddy.deb", ps.srv.URL+"/caddy.deb", []byte("x"))},
	}, nil)

	got, err := r.Resolve(context.Background(), Request{Kind: KindFile, Path: archive, Arch: "armv6"})
	if err != nil {
		t.Fatalf("Resolve: %v, a 404 is not an error", err)
	}
	if len(got.MissingPackages) != 1 || got.MissingPackages[0] != "caddy" || len(got.Packages) != 0 {
		t.Errorf("MissingPackages = %v, Packages = %+v", got.MissingPackages, got.Packages)
	}
}

// pinWithFallbacks is pin with fallback_urls set.
func pinWithFallbacks(file, url string, data []byte, fallbacks ...string) map[string]any {
	p := pin(file, url, data)
	p["fallback_urls"] = fallbacks
	return p
}

func TestResolveFallsBackWhenTheMirrorFails(t *testing.T) {
	ts := []byte("tailscale-deb-bytes")
	ps := newPinServer(t, map[string][]byte{"/ts.deb": ts})
	r := pinnedResolver(t, ps)
	var notes []string
	archive := packedBundleWithPins(t, "armv6", map[string]any{
		"packages": map[string]any{
			"tailscale": pinWithFallbacks("ts.deb", ps.srv.URL+"/mirror/ts.deb", ts, ps.srv.URL+"/ts.deb"),
		},
	}, nil)

	got, err := r.Resolve(context.Background(), Request{
		Kind: KindFile, Path: archive, Arch: "armv6",
		Note: func(key string, _ map[string]string) { notes = append(notes, key) },
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got.Packages) != 1 || len(got.MissingPackages) != 0 {
		t.Fatalf("Packages = %+v, Missing = %v, want the fallback file", got.Packages, got.MissingPackages)
	}
	if ps.requests.Load() != 2 {
		t.Errorf("made %d requests, want 2 (mirror, then fallback)", ps.requests.Load())
	}
	if !containsKey(notes, "package.log.download_fallback") {
		t.Errorf("notes = %v, want package.log.download_fallback", notes)
	}
}

func TestResolveDoesNotFallBackOnAWrongChecksum(t *testing.T) {
	ps := newPinServer(t, map[string][]byte{"/mirror.deb": []byte("tampered"), "/ts.deb": []byte("original")})
	r := pinnedResolver(t, ps)
	archive := packedBundleWithPins(t, "armv6", map[string]any{
		"packages": map[string]any{
			"tailscale": pinWithFallbacks("ts.deb", ps.srv.URL+"/mirror.deb", []byte("original"), ps.srv.URL+"/ts.deb"),
		},
	}, nil)

	_, err := r.Resolve(context.Background(), Request{Kind: KindFile, Path: archive, Arch: "armv6"})
	if code(err) != CodePackageDownloadInvalid {
		t.Fatalf("err = %v, want %s", err, CodePackageDownloadInvalid)
	}
	if ps.requests.Load() != 1 {
		t.Errorf("made %d requests, want 1: a wrong checksum must not reach the fallback", ps.requests.Load())
	}
	cached := filepath.Join(r.CacheDir, "packages", sha256Hex([]byte("original")), "ts.deb")
	if _, statErr := os.Stat(cached); !os.IsNotExist(statErr) {
		t.Errorf("a cache file with the wrong checksum must be deleted (stat err = %v)", statErr)
	}
}

func TestResolveRejectsAFallbackFromAnUnknownHost(t *testing.T) {
	r := newResolver(t, nil)
	r.CacheDir = t.TempDir()
	archive := packedBundleWithPins(t, "armv6", map[string]any{
		"packages": map[string]any{"tailscale": map[string]any{
			"version": "1.62.0", "file": "tailscale_1.62.0_armhf.deb",
			"url":           "https://github.com/Developer-Simon/energy-node/releases/download/pinned-packages/tailscale_1.62.0_armhf.deb",
			"fallback_urls": []string{"https://evil.example/tailscale_1.62.0_armhf.deb"},
			"sha256":        sha256Hex([]byte("x")), "update": "installer",
		}},
	}, nil)

	_, err := r.Resolve(context.Background(), Request{Kind: KindFile, Path: archive, Arch: "armv6"})
	if code(err) != CodePackageURLRejected {
		t.Fatalf("err = %v, want %s", err, CodePackageURLRejected)
	}
}

func TestResolveRecordsMissingWhenAllSourcesFail(t *testing.T) {
	ps := newPinServer(t, map[string][]byte{})
	r := pinnedResolver(t, ps)
	archive := packedBundleWithPins(t, "armv6", map[string]any{
		"packages": map[string]any{
			"tailscale": pinWithFallbacks("ts.deb", ps.srv.URL+"/mirror/ts.deb", []byte("x"), ps.srv.URL+"/ts.deb"),
		},
	}, nil)

	got, err := r.Resolve(context.Background(), Request{Kind: KindFile, Path: archive, Arch: "armv6"})
	if err != nil {
		t.Fatalf("Resolve: %v, failed transfers are not an error", err)
	}
	if len(got.MissingPackages) != 1 || got.MissingPackages[0] != "tailscale" || len(got.Packages) != 0 {
		t.Errorf("MissingPackages = %v, Packages = %+v", got.MissingPackages, got.Packages)
	}
	if ps.requests.Load() != 2 {
		t.Errorf("made %d requests, want 2 (mirror, then fallback)", ps.requests.Load())
	}
}

func containsKey(keys []string, want string) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}
	return false
}

func TestResolveWithoutPackagesDoesNothing(t *testing.T) {
	ps := newPinServer(t, map[string][]byte{})
	r := pinnedResolver(t, ps)
	archive := packedBundleWithPins(t, "armv6", nil, nil)

	got, err := r.Resolve(context.Background(), Request{Kind: KindFile, Path: archive, Arch: "armv6"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got.Packages) != 0 || len(got.MissingPackages) != 0 || len(got.Wheels) != 0 || len(got.MissingWheels) != 0 {
		t.Errorf("an old manifest must leave every list empty: %+v", got)
	}
	if ps.requests.Load() != 0 {
		t.Errorf("made %d HTTP requests for a manifest without pins", ps.requests.Load())
	}
}

func wheelPin(file, url string, data []byte) map[string]any {
	return map[string]any{"file": file, "url": url, "sha256": sha256Hex(data)}
}

func TestResolveFetchesPinnedWheels(t *testing.T) {
	wheel := []byte("wheel-bytes")
	ps := newPinServer(t, map[string][]byte{"/pkg.whl": wheel})
	r := pinnedResolver(t, ps)
	archive := packedBundleWithPins(t, "armv6", map[string]any{
		"wheels": []any{wheelPin("pkg-1.0-py3-none-any.whl", ps.srv.URL+"/pkg.whl", wheel)},
	}, nil)

	got, err := r.Resolve(context.Background(), Request{Kind: KindFile, Path: archive, Arch: "armv6"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got.Wheels) != 1 || got.Wheels[0].Name != "pkg-1.0-py3-none-any.whl" {
		t.Fatalf("Wheels = %+v", got.Wheels)
	}
	if _, err := os.Stat(got.Wheels[0].Path); err != nil {
		t.Errorf("the wheel is not in the cache: %v", err)
	}
}

func TestResolveSkipsEmbeddedWheels(t *testing.T) {
	wheel := []byte("embedded-wheel")
	ps := newPinServer(t, map[string][]byte{"/pkg.whl": wheel})
	r := pinnedResolver(t, ps)
	archive := packedBundleWithPins(t, "armv6", map[string]any{
		"wheels": []any{wheelPin("pkg-1.0-py3-none-any.whl", ps.srv.URL+"/pkg.whl", wheel)},
	}, map[string][]byte{"pkg-1.0-py3-none-any.whl": wheel})

	got, err := r.Resolve(context.Background(), Request{Kind: KindFile, Path: archive, Arch: "armv6"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got.Wheels) != 0 || len(got.MissingWheels) != 0 {
		t.Errorf("Wheels = %+v, Missing = %v, want nothing for an embedded wheel", got.Wheels, got.MissingWheels)
	}
	if ps.requests.Load() != 0 {
		t.Errorf("an embedded wheel must not be downloaded (%d requests)", ps.requests.Load())
	}
}

func TestResolveRecordsAnUnreachableWheelAsMissing(t *testing.T) {
	ps := newPinServer(t, map[string][]byte{})
	r := pinnedResolver(t, ps)
	archive := packedBundleWithPins(t, "armv6", map[string]any{
		"wheels": []any{wheelPin("pkg-1.0-py3-none-any.whl", ps.srv.URL+"/pkg.whl", []byte("x"))},
	}, nil)

	got, err := r.Resolve(context.Background(), Request{Kind: KindFile, Path: archive, Arch: "armv6"})
	if err != nil {
		t.Fatalf("Resolve: %v, a failed wheel download is not an error", err)
	}
	if len(got.MissingWheels) != 1 || got.MissingWheels[0] != "pkg-1.0-py3-none-any.whl" {
		t.Errorf("MissingWheels = %v", got.MissingWheels)
	}
}

func TestResolveRejectsAWheelWithAWrongChecksum(t *testing.T) {
	ps := newPinServer(t, map[string][]byte{"/pkg.whl": []byte("tampered")})
	r := pinnedResolver(t, ps)
	archive := packedBundleWithPins(t, "armv6", map[string]any{
		"wheels": []any{wheelPin("pkg-1.0-py3-none-any.whl", ps.srv.URL+"/pkg.whl", []byte("original"))},
	}, nil)

	_, err := r.Resolve(context.Background(), Request{Kind: KindFile, Path: archive, Arch: "armv6"})
	if code(err) != CodePackageDownloadInvalid {
		t.Fatalf("err = %v, want %s", err, CodePackageDownloadInvalid)
	}
}

func TestPinnedDownloadsNeverRunMoreThanFourAtOnce(t *testing.T) {
	var inFlight, peak atomic.Int32
	data := map[string][]byte{}
	pins := map[string]any{}
	for i := 0; i < 9; i++ {
		name := fmt.Sprintf("p%d", i)
		body := []byte("pkg-" + name)
		data["/"+name+".deb"] = body
		pins[name] = pin(name+".deb", "", body)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		body, ok := data[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	for name, p := range pins {
		p.(map[string]any)["url"] = srv.URL + "/" + name + ".deb"
	}
	r := pinnedResolver(t, &pinServer{srv: srv})
	archive := packedBundleWithPins(t, "armv6", map[string]any{"packages": pins}, nil)

	got, err := r.Resolve(context.Background(), Request{Kind: KindFile, Path: archive, Arch: "armv6"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got.Packages) != 9 {
		t.Errorf("Packages = %d, want 9", len(got.Packages))
	}
	if peak.Load() > maxParallelDownloads || peak.Load() < 2 {
		t.Errorf("peak concurrent downloads = %d, want between 2 and %d", peak.Load(), maxParallelDownloads)
	}
}

func TestRedirectPolicyAllowsGitHubAssetHostsOnlyForAGitHubPin(t *testing.T) {
	redirect := func(raw string) *http.Request {
		u, _ := url.Parse(raw)
		return &http.Request{URL: u}
	}
	fromGitHub := redirectPolicy(defaultPackageRules, allowedDebHosts, "github.com")
	if err := fromGitHub(redirect("https://objects.githubusercontent.com/x.deb"), nil); err != nil {
		t.Errorf("a GitHub pin may redirect to a release-asset host: %v", err)
	}
	if err := fromGitHub(redirect("https://evil.example/x.deb"), nil); code(err) != CodePackageURLRejected {
		t.Errorf("err = %v, want %s", err, CodePackageURLRejected)
	}
	if err := fromGitHub(redirect("http://objects.githubusercontent.com/x.deb"), nil); code(err) != CodePackageURLRejected {
		t.Errorf("a redirect to plain HTTP must be refused, err = %v", err)
	}
	fromTailscale := redirectPolicy(defaultPackageRules, allowedDebHosts, "pkgs.tailscale.com")
	if err := fromTailscale(redirect("https://objects.githubusercontent.com/x.deb"), nil); code(err) != CodePackageURLRejected {
		t.Errorf("a Tailscale pin must not follow to GitHub, err = %v", err)
	}
}
