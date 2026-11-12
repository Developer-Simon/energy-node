package media

import (
	"bytes"
	"context"
	"crypto/sha256"
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
