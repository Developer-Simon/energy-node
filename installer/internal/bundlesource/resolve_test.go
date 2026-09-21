package bundlesource

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
)

// writeBundle writes a minimal, hash-consistent bundle into dir. A non-nil
// key also writes manifest.json.sig.
func writeBundle(t *testing.T, dir, arch string, key ed25519.PrivateKey) {
	t.Helper()
	body := []byte("#!/bin/sh\n")
	sum := sha256.Sum256(body)
	if err := os.MkdirAll(filepath.Join(dir, "bootstrap"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bootstrap", "x.sh"), body, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest, _ := json.Marshal(map[string]any{
		"version": "v9.9.9", "arch": arch, "uname_machine": []string{"armv6l"},
		"steps": []any{}, "files": map[string]string{"bootstrap/x.sh": hex.EncodeToString(sum[:])},
	})
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if key != nil {
		if err := os.WriteFile(filepath.Join(dir, "manifest.json.sig"), ed25519.Sign(key, manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func packedBundle(t *testing.T, arch string, key ed25519.PrivateKey) string {
	t.Helper()
	src := t.TempDir()
	writeBundle(t, src, arch, key)
	archive := filepath.Join(t.TempDir(), "energy-node-v9.9.9-"+arch+".tar.gz")
	if err := bundle.PackDir(src, archive); err != nil {
		t.Fatal(err)
	}
	return archive
}

func newResolver(t *testing.T, pub ed25519.PublicKey) *Resolver {
	return &Resolver{
		BundledDir: t.TempDir(),
		WorkDir:    t.TempDir(),
		GitHub:     &GitHub{CacheDir: t.TempDir()},
		PublicKey:  pub,
	}
}

func code(err error) string {
	var se *Error
	if errors.As(err, &se) {
		return se.Code
	}
	var be *bundle.Error
	if errors.As(err, &be) {
		return string(be.Code)
	}
	return ""
}

func TestBundledSourceVerifiesTheDirectoryInPlace(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	r := newResolver(t, pub)
	writeBundle(t, r.BundledDir, "armv6", priv)

	got, err := r.Resolve(context.Background(), Request{Kind: KindBundled, Arch: "armv6"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Dir != r.BundledDir || got.ArchivePath != "" || !got.Signed {
		t.Errorf("Resolved = %+v, want the bundled dir, no archive, signed", got)
	}
	got.Cleanup()
	if _, err := os.Stat(r.BundledDir); err != nil {
		t.Errorf("Cleanup must never remove the bundled directory")
	}
}

func TestAnUnsignedFileIsAcceptedButFlagged(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	r := newResolver(t, pub)
	archive := packedBundle(t, "armv6", nil)

	got, err := r.Resolve(context.Background(), Request{Kind: KindFile, Path: archive, Arch: "armv6"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Signed || got.ArchivePath != archive || got.Manifest.Version != "v9.9.9" {
		t.Errorf("Resolved = %+v", got)
	}
	got.Cleanup()
	if _, err := os.Stat(archive); err != nil {
		t.Errorf("Cleanup must never remove the user's file")
	}
}

func TestAForgedSignatureIsNeverDowngradedToUnsigned(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	_, otherKey, _ := ed25519.GenerateKey(rand.Reader)
	r := newResolver(t, pub)
	archive := packedBundle(t, "armv6", otherKey) // signed, but not by the release key

	_, err := r.Resolve(context.Background(), Request{Kind: KindFile, Path: archive, Arch: "armv6"})
	if code(err) != string(bundle.FaultSignatureInvalid) {
		t.Fatalf("err = %v, want %s", err, bundle.FaultSignatureInvalid)
	}
}

func TestAnArchMismatchIsRejectedBeforeAnythingIsStaged(t *testing.T) {
	r := newResolver(t, nil)
	archive := packedBundle(t, "amd64", nil)
	_, err := r.Resolve(context.Background(), Request{Kind: KindFile, Path: archive, Arch: "armv6"})
	if code(err) != string(bundle.FaultArchMismatch) {
		t.Fatalf("err = %v, want %s", err, bundle.FaultArchMismatch)
	}
}

func TestANonArchiveFileIsPackageFileInvalid(t *testing.T) {
	r := newResolver(t, nil)
	bad := filepath.Join(t.TempDir(), "x.tar.gz")
	os.WriteFile(bad, []byte("not gzip"), 0o644)
	_, err := r.Resolve(context.Background(), Request{Kind: KindFile, Path: bad, Arch: "armv6"})
	if code(err) != CodePackageFileInvalid {
		t.Fatalf("err = %v, want %s", err, CodePackageFileInvalid)
	}
}

func TestGitHubRequiresTheReleaseSignatureAndDropsABadCachedArchive(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	good := packedBundle(t, "armv6", priv)
	unsigned := packedBundle(t, "armv6", nil)

	serve := func(file string) *httptest.Server {
		var srv *httptest.Server
		mux := http.NewServeMux()
		mux.HandleFunc("/repos/o/r/releases", func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte(`[{"tag_name":"v9.9.9","draft":false,"prerelease":false,"assets":[
			  {"name":"energy-node-v9.9.9-armv6.tar.gz","browser_download_url":"` + srv.URL + `/dl"}]}]`))
		})
		mux.HandleFunc("/dl", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, file) })
		srv = httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		return srv
	}

	// A correctly signed release resolves as signed.
	srv := serve(good)
	r := newResolver(t, pub)
	r.GitHub = &GitHub{APIBase: srv.URL, Repo: "o/r", CacheDir: t.TempDir()}
	got, err := r.Resolve(context.Background(), Request{Kind: KindGitHub, Arch: "armv6"})
	if err != nil || !got.Signed {
		t.Fatalf("Resolve = %+v, %v; want a signed bundle", got, err)
	}

	// An unsigned release is refused, and its cached archive is removed.
	srv2 := serve(unsigned)
	r2 := newResolver(t, pub)
	r2.GitHub = &GitHub{APIBase: srv2.URL, Repo: "o/r", CacheDir: t.TempDir()}
	_, err = r2.Resolve(context.Background(), Request{Kind: KindGitHub, Arch: "armv6"})
	if code(err) != string(bundle.FaultSignatureInvalid) {
		t.Fatalf("err = %v, want %s", err, bundle.FaultSignatureInvalid)
	}
	cached := filepath.Join(r2.GitHub.CacheDir, "v9.9.9", "energy-node-v9.9.9-armv6.tar.gz")
	if _, statErr := os.Stat(cached); !os.IsNotExist(statErr) {
		t.Errorf("the cached archive that failed verification must be removed (stat err = %v)", statErr)
	}
}

func TestRepoSourceBuildsForTheNodeArchAndStreamsTheLog(t *testing.T) {
	r := newResolver(t, nil)
	checkout := fakeCheckout(t)
	built := packedBundle(t, "armv6", nil)

	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	lookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }

	var gotArgs bundle.BuildArgs
	r.Build = func(_ context.Context, args bundle.BuildArgs) (string, error) {
		gotArgs = args
		args.Log("compiling dashboard")
		dest := filepath.Join(args.OutDir, filepath.Base(built))
		data, _ := os.ReadFile(built)
		return dest, os.WriteFile(dest, data, 0o644)
	}
	var lines []string
	got, err := r.Resolve(context.Background(), Request{
		Kind: KindRepo, Path: checkout, Arch: "armv6", User: "pi", Base: "/home/pi",
		Log: func(l string) { lines = append(lines, l) },
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if gotArgs.RepoRoot != checkout || gotArgs.Arch != "armv6" || gotArgs.User != "pi" || gotArgs.Base != "/home/pi" {
		t.Errorf("BuildArgs = %+v", gotArgs)
	}
	if got.Signed {
		t.Errorf("a local build without a key is unsigned")
	}
	found := false
	for _, l := range lines {
		found = found || l == "compiling dashboard"
	}
	if !found {
		t.Errorf("the build log was not forwarded: %q", lines)
	}
}

func TestRepoBuildFailureIsBuildFailedWithTheCauseAsDetail(t *testing.T) {
	r := newResolver(t, nil)
	checkout := fakeCheckout(t)
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	lookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	r.Build = func(context.Context, bundle.BuildArgs) (string, error) { return "", errors.New("boom") }

	_, err := r.Resolve(context.Background(), Request{Kind: KindRepo, Path: checkout, Arch: "armv6"})
	if code(err) != CodeBuildFailed {
		t.Fatalf("err = %v, want %s", err, CodeBuildFailed)
	}
}
