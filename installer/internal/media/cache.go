package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrChecksum reports that a file does not match its SHA-256 checksum.
var ErrChecksum = errors.New("media: checksum mismatch")

const (
	osListFile = "os_list.json"
	imagesDir  = "images"
)

// datePrefix matches the release date at the start of an image file name.
var datePrefix = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-`)

// Cache keeps the OS list and the downloaded images below Dir.
type Cache struct {
	Dir       string       // z. B. <UserCacheDir>/energy-node-installer/media
	Client    *http.Client // nil = http.DefaultClient
	OSListURL string       // leer = OSListURL
}

func (c *Cache) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return http.DefaultClient
}

func (c *Cache) osListURL() string {
	if c.OSListURL != "" {
		return c.OSListURL
	}
	return OSListURL
}

// OSList fetches the OS list and stores it as Dir/os_list.json. Without network
// it falls back to the stored copy and reports offline=true. If both are
// missing, it returns an error.
func (c *Cache) OSList(ctx context.Context) (*OSList, bool, error) {
	data, netErr := c.get(ctx, c.osListURL())
	if netErr == nil {
		list, err := ParseOSList(data)
		if err == nil {
			if err := os.MkdirAll(c.Dir, 0o755); err != nil {
				return nil, false, fmt.Errorf("create cache dir: %w", err)
			}
			if err := os.WriteFile(filepath.Join(c.Dir, osListFile), data, 0o644); err != nil {
				return nil, false, fmt.Errorf("store OS list: %w", err)
			}
			return list, false, nil
		}
		netErr = err
	}

	stored, err := os.ReadFile(filepath.Join(c.Dir, osListFile))
	if err != nil {
		return nil, false, fmt.Errorf("OS list unavailable (%v) and no stored copy: %w", netErr, err)
	}
	list, err := ParseOSList(stored)
	if err != nil {
		return nil, false, fmt.Errorf("stored OS list unusable: %w", err)
	}
	return list, true, nil
}

// Cached is a cheap check that the image and its .sha256 file exist as regular
// files and that the image size equals img.DownloadSize when that is known. It
// does not hash the image, because the catalog calls it once per device. Ensure
// verifies the content on every call.
func (c *Cache) Cached(img Image) bool {
	name, err := imageName(img)
	if err != nil {
		return false
	}
	file := filepath.Join(c.Dir, imagesDir, name)
	fi, err := os.Stat(file)
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	si, err := os.Stat(file + ".sha256")
	if err != nil || !si.Mode().IsRegular() {
		return false
	}
	return img.DownloadSize <= 0 || fi.Size() == img.DownloadSize
}

// Ensure returns the local path of the image. It downloads the image when it is
// not cached, verifies it against the published SHA-256 file and removes older
// releases of the same image. progress reports the download, verify the check.
// A cached image is hashed again on every call.
func (c *Cache) Ensure(ctx context.Context, img Image, progress, verify func(done, total int64)) (string, error) {
	name, err := imageName(img)
	if err != nil {
		return "", err
	}
	images := filepath.Join(c.Dir, imagesDir)
	file := filepath.Join(images, name)

	if want, err := readSum(file + ".sha256"); err == nil {
		if _, err := os.Stat(file); err == nil {
			got, err := hashFile(file, verify)
			if err != nil {
				return "", err
			}
			if got != want {
				_ = os.Remove(file)
				_ = os.Remove(file + ".sha256")
				return "", fmt.Errorf("%s: %w", name, ErrChecksum)
			}
			if err := prune(images, name); err != nil {
				return "", err
			}
			return file, nil
		}
	}

	if err := c.download(ctx, img, file, progress, verify); err != nil {
		return "", err
	}
	if err := prune(images, name); err != nil {
		return "", err
	}
	return file, nil
}

// download fetches the image into file+".part", verifies it and renames it into
// place. The checksum file is stored only after the image has passed.
func (c *Cache) download(ctx context.Context, img Image, file string, progress, verify func(done, total int64)) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return fmt.Errorf("create images dir: %w", err)
	}
	sumData, err := c.get(ctx, img.URL+".sha256")
	if err != nil {
		return fmt.Errorf("fetch checksum: %w", err)
	}
	want, err := parseSum(sumData)
	if err != nil {
		return err
	}

	resp, err := c.getStream(ctx, img.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	total := resp.ContentLength
	if total < 0 {
		total = img.DownloadSize
	}
	var body io.Reader = resp.Body
	if progress != nil {
		body = &progressReader{r: resp.Body, total: total, fn: progress}
	}

	part := file + ".part"
	out, err := os.Create(part)
	if err != nil {
		return fmt.Errorf("create download: %w", err)
	}
	if _, err := io.Copy(out, body); err != nil {
		out.Close()
		return fmt.Errorf("download %s: %w", filepath.Base(file), err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("write download: %w", err)
	}

	got, err := hashFile(part, verify)
	if err != nil {
		return err
	}
	if got != want {
		_ = os.Remove(part)
		return fmt.Errorf("%s: %w", filepath.Base(file), ErrChecksum)
	}
	if err := os.WriteFile(file+".sha256", sumData, 0o644); err != nil {
		return fmt.Errorf("store checksum: %w", err)
	}
	if err := os.Rename(part, file); err != nil {
		return fmt.Errorf("finish download: %w", err)
	}
	return nil
}

// imageName returns the base name of the image URL.
func imageName(img Image) (string, error) {
	u, err := url.Parse(img.URL)
	if err != nil {
		return "", fmt.Errorf("image URL: %w", err)
	}
	name := path.Base(u.Path)
	if name == "." || name == "/" {
		return "", fmt.Errorf("image URL %q has no file name", img.URL)
	}
	return name, nil
}

// prune removes older releases of the image name in dir. Two names belong
// together when they match after the release date, for example
// "2025-05-13-raspios-bookworm-armhf-lite.img.xz" and
// "2026-10-06-raspios-bookworm-armhf-lite.img.xz".
func prune(dir, keep string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read images dir: %w", err)
	}
	key := releaseKey(keep)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == keep || name == keep+".sha256" || releaseKey(name) != key {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("remove old release: %w", err)
		}
	}
	return nil
}

// releaseKey strips the release date, the checksum suffix and the partial
// download suffix from an image file name.
func releaseKey(name string) string {
	name = strings.TrimSuffix(name, ".part")
	name = strings.TrimSuffix(name, ".sha256")
	return datePrefix.ReplaceAllString(name, "")
}

// readSum reads the checksum from a .sha256 file.
func readSum(file string) (string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	return parseSum(data)
}

// parseSum returns the first token of a .sha256 file, which is the hex digest.
func parseSum(data []byte) (string, error) {
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return "", errors.New("empty checksum file")
	}
	sum := strings.ToLower(fields[0])
	if len(sum) != hex.EncodedLen(sha256.Size) {
		return "", fmt.Errorf("invalid checksum %q", fields[0])
	}
	if _, err := hex.DecodeString(sum); err != nil {
		return "", fmt.Errorf("invalid checksum %q: %w", fields[0], err)
	}
	return sum, nil
}

// hashFile returns the hex SHA-256 of the file. verify, when set, reports progress.
func hashFile(file string, verify func(done, total int64)) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", filepath.Base(file), err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	var r io.Reader = f
	if verify != nil {
		r = &progressReader{r: f, total: st.Size(), fn: verify}
	}
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", fmt.Errorf("hash %s: %w", filepath.Base(file), err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// progressReader reports the bytes read so far after every read.
type progressReader struct {
	r     io.Reader
	total int64
	done  int64
	fn    func(done, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if n > 0 {
		p.fn(p.done, p.total)
	}
	return n, err
}

// get fetches a small document and returns its body.
func (c *Cache) get(ctx context.Context, rawURL string) ([]byte, error) {
	resp, err := c.getStream(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// getStream starts a GET request and fails on any status other than 200.
func (c *Cache) getStream(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	return resp, nil
}
