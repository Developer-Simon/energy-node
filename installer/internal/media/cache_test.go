package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

const testImageName = "2026-10-06-raspios-bookworm-armhf-lite.img.xz"

// serveImage serves body as the image and its .sha256 file. An empty sum means
// the correct checksum of body. hits, when set, counts image requests.
func serveImage(t *testing.T, body []byte, sum string, hits *int32) *httptest.Server {
	t.Helper()
	if sum == "" {
		d := sha256.Sum256(body)
		sum = hex.EncodeToString(d[:])
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/images/" + testImageName:
			if hits != nil {
				atomic.AddInt32(hits, 1)
			}
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
			w.Write(body)
		case "/images/" + testImageName + ".sha256":
			fmt.Fprintf(w, "%s  %s\n", sum, testImageName)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testImage(srv *httptest.Server, size int64) Image {
	return Image{URL: srv.URL + "/images/" + testImageName, DownloadSize: size}
}

func testBody() []byte {
	body := make([]byte, 1<<20)
	for i := range body {
		body[i] = byte(i * 7)
	}
	return body
}

func TestEnsureDownloadsAndVerifies(t *testing.T) {
	body := testBody()
	srv := serveImage(t, body, "", nil)
	cache := &Cache{Dir: t.TempDir()}

	var total int64
	progress := func(done, size int64) { total = size }
	p, err := cache.Ensure(context.Background(), testImage(srv, int64(len(body))), progress, nil)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("result file: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Error("downloaded image differs from the served bytes")
	}
	if total != 1<<20 {
		t.Errorf("progress total = %d, want %d", total, 1<<20)
	}
}

func TestEnsureRejectsAWrongChecksum(t *testing.T) {
	dir := t.TempDir()
	srv := serveImage(t, testBody(), strings.Repeat("0", 64), nil)
	cache := &Cache{Dir: dir}

	_, err := cache.Ensure(context.Background(), testImage(srv, 0), nil, nil)
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("err = %v, want ErrChecksum", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "images"))
	if err != nil {
		t.Fatalf("read images dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("images dir has %d leftover files, want none", len(entries))
	}
}

func TestEnsureUsesTheCacheButRehashes(t *testing.T) {
	body := testBody()
	var hits int32
	srv := serveImage(t, body, "", &hits)
	cache := &Cache{Dir: t.TempDir()}
	img := testImage(srv, int64(len(body)))
	ctx := context.Background()

	p, err := cache.Ensure(ctx, img, nil, nil)
	if err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	if _, err := cache.Ensure(ctx, img, nil, nil); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("image requests = %d, want 1 (second call must use the cache)", n)
	}

	f, err := os.OpenFile(p, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open cached image: %v", err)
	}
	if _, err := f.WriteAt([]byte{body[0] ^ 0xff}, 0); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	f.Close()

	if _, err := cache.Ensure(ctx, img, nil, nil); !errors.Is(err, ErrChecksum) {
		t.Fatalf("tampered Ensure err = %v, want ErrChecksum", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("tampered image still present, stat err = %v", err)
	}
}

func TestCachedDoesNotHash(t *testing.T) {
	body := testBody()
	srv := serveImage(t, body, "", nil)
	cache := &Cache{Dir: t.TempDir()}
	img := testImage(srv, int64(len(body)))
	ctx := context.Background()

	p, err := cache.Ensure(ctx, img, nil, nil)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !cache.Cached(img) {
		t.Fatal("Cached false after a verified download")
	}

	f, err := os.OpenFile(p, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open cached image: %v", err)
	}
	if _, err := f.WriteAt([]byte{body[0] ^ 0xff}, 0); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	f.Close()

	if !cache.Cached(img) {
		t.Error("Cached hashed the image: a same-size tampered file must still be reported cached")
	}
	if _, err := cache.Ensure(ctx, img, nil, nil); !errors.Is(err, ErrChecksum) {
		t.Fatalf("Ensure err = %v, want ErrChecksum", err)
	}
}

func TestEnsureDropsOlderReleasesOfTheSameImage(t *testing.T) {
	dir := t.TempDir()
	images := filepath.Join(dir, "images")
	if err := os.MkdirAll(images, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(images, "2025-05-13-raspios-bookworm-armhf-lite.img.xz")
	oldSum := old + ".sha256"
	other := filepath.Join(images, "x.img.xz")
	for _, f := range []string{old, oldSum, other} {
		if err := os.WriteFile(f, []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	srv := serveImage(t, testBody(), "", nil)
	cache := &Cache{Dir: dir}
	p, err := cache.Ensure(context.Background(), testImage(srv, 0), nil, nil)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	for _, gone := range []string{old, oldSum} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("older release %s still present", filepath.Base(gone))
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("unrelated file was removed: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("current image missing: %v", err)
	}
}

func TestCatalogFallsBackToTheStoredListOffline(t *testing.T) {
	data, err := os.ReadFile("testdata/os_list.json")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	}))
	cache := &Cache{Dir: t.TempDir(), OSListURL: srv.URL + "/os_list_imagingutility_v4.json"}
	ctx := context.Background()

	first, offline, err := cache.OSList(ctx)
	if err != nil {
		t.Fatalf("online OSList: %v", err)
	}
	if offline {
		t.Fatal("online OSList reported offline")
	}

	srv.Close()
	second, offline, err := cache.OSList(ctx)
	if err != nil {
		t.Fatalf("offline OSList: %v", err)
	}
	if !offline {
		t.Error("offline OSList did not report offline")
	}
	if !reflect.DeepEqual(first, second) {
		t.Error("stored list differs from the fetched one")
	}
}

func TestOSListWithoutNetAndWithoutStoreFails(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	cache := &Cache{Dir: t.TempDir(), OSListURL: url + "/os_list_imagingutility_v4.json"}

	if _, _, err := cache.OSList(context.Background()); err == nil {
		t.Fatal("expected an error without network and without a stored list")
	}
}

const testISOName = "debian-13.7.0-amd64-netinst.iso"

// testRelease serves body as the ISO called name and returns the release that
// the signed SHA512SUMS would list for it. An empty sum means the correct
// SHA-512 of body. hits, when set, counts ISO requests.
func testRelease(t *testing.T, name string, body []byte, sum string, hits *int32) DebianRelease {
	t.Helper()
	if sum == "" {
		d := sha512.Sum512(body)
		sum = hex.EncodeToString(d[:])
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+name {
			http.NotFound(w, r)
			return
		}
		if hits != nil {
			atomic.AddInt32(hits, 1)
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return DebianRelease{Name: name, URL: srv.URL + "/" + name, SHA512: sum, Size: int64(len(body))}
}

func TestEnsureISODownloadsVerifiesAndStores(t *testing.T) {
	body := testBody()
	r := testRelease(t, testISOName, body, "", nil)
	cache := &Cache{Dir: t.TempDir()}
	if cache.CachedISO(r) {
		t.Fatal("CachedISO true before the download")
	}

	var total int64
	progress := func(done, size int64) { total = size }
	p, err := cache.EnsureISO(context.Background(), r, progress, nil)
	if err != nil {
		t.Fatalf("EnsureISO: %v", err)
	}
	if want := filepath.Join(cache.Dir, "images", testISOName); p != want {
		t.Errorf("path = %q, want %q", p, want)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("result file: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Error("downloaded ISO differs from the served bytes")
	}
	if total != int64(len(body)) {
		t.Errorf("progress total = %d, want %d", total, len(body))
	}
	if _, err := os.Stat(p + ".sha512"); err != nil {
		t.Errorf("checksum file missing: %v", err)
	}
	if !cache.CachedISO(r) {
		t.Error("CachedISO false after a verified download")
	}
	wrongSize := r
	wrongSize.Size++
	if cache.CachedISO(wrongSize) {
		t.Error("CachedISO true for a size that differs from the cached file")
	}
}

func TestEnsureISORejectsAWrongDigestAndDeletesTheFile(t *testing.T) {
	dir := t.TempDir()
	r := testRelease(t, testISOName, testBody(), strings.Repeat("0", 128), nil)
	cache := &Cache{Dir: dir}

	_, err := cache.EnsureISO(context.Background(), r, nil, nil)
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("err = %v, want ErrChecksum", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "images"))
	if err != nil {
		t.Fatalf("read images dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("images dir has %d leftover files, want none", len(entries))
	}
}

func TestEnsureISOHashesACachedFileAgain(t *testing.T) {
	body := testBody()
	var hits int32
	r := testRelease(t, testISOName, body, "", &hits)
	cache := &Cache{Dir: t.TempDir()}
	ctx := context.Background()

	p, err := cache.EnsureISO(ctx, r, nil, nil)
	if err != nil {
		t.Fatalf("first EnsureISO: %v", err)
	}
	if _, err := cache.EnsureISO(ctx, r, nil, nil); err != nil {
		t.Fatalf("second EnsureISO: %v", err)
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("ISO requests = %d, want 1 (second call must use the cache)", n)
	}

	f, err := os.OpenFile(p, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open cached ISO: %v", err)
	}
	if _, err := f.WriteAt([]byte{body[0] ^ 0xff}, 0); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	f.Close()

	if _, err := cache.EnsureISO(ctx, r, nil, nil); !errors.Is(err, ErrChecksum) {
		t.Fatalf("tampered EnsureISO err = %v, want ErrChecksum", err)
	}
	for _, gone := range []string{p, p + ".sha512"} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("tampered ISO file %s still present, stat err = %v", filepath.Base(gone), err)
		}
	}
}

func TestPruneKeepsOnlyTheNewestISOPerArch(t *testing.T) {
	dir := t.TempDir()
	images := filepath.Join(dir, "images")
	if err := os.MkdirAll(images, 0o755); err != nil {
		t.Fatal(err)
	}
	oldAmd := "debian-13.6.0-amd64-netinst.iso"
	oldArm := "debian-13.6.0-arm64-netinst.iso"
	for _, name := range []string{oldAmd, oldArm} {
		for _, f := range []string{name, name + ".sha512"} {
			if err := os.WriteFile(filepath.Join(images, f), []byte("old"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	cache := &Cache{Dir: dir}
	ctx := context.Background()

	amd := testRelease(t, "debian-13.7.0-amd64-netinst.iso", testBody(), "", nil)
	if _, err := cache.EnsureISO(ctx, amd, nil, nil); err != nil {
		t.Fatalf("amd64 EnsureISO: %v", err)
	}
	for _, gone := range []string{oldAmd, oldAmd + ".sha512"} {
		if _, err := os.Stat(filepath.Join(images, gone)); !os.IsNotExist(err) {
			t.Errorf("older amd64 release %s still present", gone)
		}
	}
	for _, kept := range []string{oldArm, oldArm + ".sha512"} {
		if _, err := os.Stat(filepath.Join(images, kept)); err != nil {
			t.Errorf("arm64 release %s was touched by the amd64 download: %v", kept, err)
		}
	}

	arm := testRelease(t, "debian-13.7.0-arm64-netinst.iso", []byte("arm64 installer"), "", nil)
	if _, err := cache.EnsureISO(ctx, arm, nil, nil); err != nil {
		t.Fatalf("arm64 EnsureISO: %v", err)
	}
	for _, gone := range []string{oldArm, oldArm + ".sha512"} {
		if _, err := os.Stat(filepath.Join(images, gone)); !os.IsNotExist(err) {
			t.Errorf("older arm64 release %s still present", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(images, amd.Name)); err != nil {
		t.Errorf("newest amd64 ISO was removed by the arm64 download: %v", err)
	}
}

func TestPruneStillHandlesDatedPiImages(t *testing.T) {
	dir := t.TempDir()
	images := filepath.Join(dir, "images")
	if err := os.MkdirAll(images, 0o755); err != nil {
		t.Fatal(err)
	}
	oldPi := "2025-05-13-raspios-bookworm-armhf-lite.img.xz"
	iso := "debian-13.6.0-amd64-netinst.iso"
	for _, f := range []string{oldPi, oldPi + ".sha256", iso, iso + ".sha512"} {
		if err := os.WriteFile(filepath.Join(images, f), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := prune(images, testImageName); err != nil {
		t.Fatalf("prune: %v", err)
	}
	for _, gone := range []string{oldPi, oldPi + ".sha256"} {
		if _, err := os.Stat(filepath.Join(images, gone)); !os.IsNotExist(err) {
			t.Errorf("dated Pi release %s still present", gone)
		}
	}
	for _, kept := range []string{iso, iso + ".sha512"} {
		if _, err := os.Stat(filepath.Join(images, kept)); err != nil {
			t.Errorf("Debian file %s removed by the Pi prune: %v", kept, err)
		}
	}
}
