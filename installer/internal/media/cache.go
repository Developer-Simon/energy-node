package media

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
)

// ErrChecksum reports that a file does not match its SHA-256 checksum.
var ErrChecksum = errors.New("media: checksum mismatch")

const (
	osListFile = "os_list.json"
	imagesDir  = "images"
)

// datePrefix matches the release date at the start of an image file name.
var datePrefix = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-`)

// debianVersion matches the version in a Debian ISO name, for example "13.7.0-".
var debianVersion = regexp.MustCompile(`^debian-\d+(?:\.\d+)*-`)

// Cache keeps the OS list and the downloaded images below Dir.
type Cache struct {
	Dir       string             // z. B. <UserCacheDir>/energy-node-installer/media
	Client    *http.Client       // nil = http.DefaultClient
	OSListURL string             // leer = OSListURL
	DebianURL string             // leer = DebianCDURL
	DebianKey openpgp.EntityList // nil = eingebauter Debian-Schlüssel
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
	return cachedFile(file, file+".sha256", img.DownloadSize)
}

// cachedFile reports whether file and its checksum file sidecar exist as regular
// files and whether file has the size, when that is known.
func cachedFile(file, sidecar string, size int64) bool {
	fi, err := os.Stat(file)
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	si, err := os.Stat(sidecar)
	if err != nil || !si.Mode().IsRegular() {
		return false
	}
	return size <= 0 || fi.Size() == size
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
			got, err := hashFile(file, sha256.New(), verify)
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

	if err := c.fetchPart(ctx, img.URL, file, img.DownloadSize, sha256.New, want, progress, verify); err != nil {
		return err
	}
	if err := os.WriteFile(file+".sha256", sumData, 0o644); err != nil {
		return fmt.Errorf("store checksum: %w", err)
	}
	if err := os.Rename(file+".part", file); err != nil {
		return fmt.Errorf("finish download: %w", err)
	}
	return nil
}

// fetchPart downloads rawURL into file+".part" and checks its digest with newHash
// against want. A mismatch removes the partial file and returns ErrChecksum.
// size is the expected size for progress reporting when the server sends no
// Content-Length.
func (c *Cache) fetchPart(ctx context.Context, rawURL, file string, size int64, newHash func() hash.Hash, want string, progress, verify func(done, total int64)) error {
	resp, err := c.getStream(ctx, rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	total := resp.ContentLength
	if total < 0 {
		total = size
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

	got, err := hashFile(part, newHash(), verify)
	if err != nil {
		return err
	}
	if got != want {
		_ = os.Remove(part)
		return fmt.Errorf("%s: %w", filepath.Base(file), ErrChecksum)
	}
	return nil
}

// EnsureISO returns the local path of the netinst ISO that r describes. It
// downloads the ISO when it is not cached and checks it against r.SHA512 from
// the signed SHA512SUMS, so no checksum is fetched from the server. A mismatch
// removes the file and returns ErrChecksum. Older releases of the same
// architecture are removed. A cached ISO is hashed again on every call.
func (c *Cache) EnsureISO(ctx context.Context, r DebianRelease, progress, verify func(done, total int64)) (string, error) {
	name, err := isoName(r)
	if err != nil {
		return "", err
	}
	images := filepath.Join(c.Dir, imagesDir)
	file := filepath.Join(images, name)

	if fi, err := os.Stat(file); err == nil && fi.Mode().IsRegular() {
		got, err := hashFile(file, sha512.New(), verify)
		if err != nil {
			return "", err
		}
		if got != r.SHA512 {
			_ = os.Remove(file)
			_ = os.Remove(file + ".sha512")
			return "", fmt.Errorf("%s: %w", name, ErrChecksum)
		}
		if err := os.WriteFile(file+".sha512", isoSumFile(r), 0o644); err != nil {
			return "", fmt.Errorf("store checksum: %w", err)
		}
	} else if err := c.downloadISO(ctx, r, file, progress, verify); err != nil {
		return "", err
	}
	if err := prune(images, name); err != nil {
		return "", err
	}
	return file, nil
}

// downloadISO fetches the ISO of r into file+".part", verifies it against the
// signed digest and renames it into place. The checksum file is stored only
// after the ISO has passed.
func (c *Cache) downloadISO(ctx context.Context, r DebianRelease, file string, progress, verify func(done, total int64)) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return fmt.Errorf("create images dir: %w", err)
	}
	if err := c.fetchPart(ctx, r.URL, file, r.Size, sha512.New, r.SHA512, progress, verify); err != nil {
		return err
	}
	if err := os.WriteFile(file+".sha512", isoSumFile(r), 0o644); err != nil {
		return fmt.Errorf("store checksum: %w", err)
	}
	if err := os.Rename(file+".part", file); err != nil {
		return fmt.Errorf("finish download: %w", err)
	}
	return nil
}

// CachedISO is the ISO counterpart of Cached: the ISO and its .sha512 file must
// exist as regular files, and the size must equal r.Size when that is known.
// It does not hash the ISO.
func (c *Cache) CachedISO(r DebianRelease) bool {
	name, err := isoName(r)
	if err != nil {
		return false
	}
	file := filepath.Join(c.Dir, imagesDir, name)
	return cachedFile(file, file+".sha512", r.Size)
}

// isoName returns the file name of the ISO in the images dir.
func isoName(r DebianRelease) (string, error) {
	if r.Name == "" || r.Name == ".." || filepath.Base(r.Name) != r.Name {
		return "", fmt.Errorf("debian release has no usable file name %q", r.Name)
	}
	if r.SHA512 == "" {
		return "", fmt.Errorf("debian release %s has no SHA-512", r.Name)
	}
	return r.Name, nil
}

// isoSumFile returns the .sha512 file for r in sha512sum format.
func isoSumFile(r DebianRelease) []byte {
	return []byte(r.SHA512 + "  " + r.Name + "\n")
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
		if e.IsDir() || name == keep || name == keep+".sha256" || name == keep+".sha512" || releaseKey(name) != key {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("remove old release: %w", err)
		}
	}
	return nil
}

// releaseKey strips the release date, the version of a Debian ISO, the checksum
// suffixes and the partial download suffix from a file name. The key of
// "debian-13.7.0-amd64-netinst.iso" is "debian-amd64-netinst.iso" for every
// release, so each architecture keeps only its newest ISO.
func releaseKey(name string) string {
	name = strings.TrimSuffix(name, ".part")
	name = strings.TrimSuffix(name, ".sha256")
	name = strings.TrimSuffix(name, ".sha512")
	name = datePrefix.ReplaceAllString(name, "")
	return debianVersion.ReplaceAllString(name, "debian-")
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

// hashFile returns the hex digest of the file with h, which must be a fresh hash.
// verify, when set, reports progress.
func hashFile(file string, h hash.Hash, verify func(done, total int64)) (string, error) {
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

func (c *Cache) debianURL() string {
	if c.DebianURL != "" {
		return c.DebianURL
	}
	return DebianCDURL
}

// DebianRelease finds the netinst ISO for arch in the Debian CD directory. It
// fetches SHA512SUMS and SHA512SUMS.sign, verifies the signature and stores both
// files as Dir/debian-<arch>.SHA512SUMS(.sign). Nothing is stored before the
// signature has passed. Without network it verifies the stored copies again and
// reports offline=true. Size comes from a HEAD request on the ISO, or from the
// cached ISO file offline, and is 0 when neither is available.
func (c *Cache) DebianRelease(ctx context.Context, arch string) (DebianRelease, bool, error) {
	keys := c.DebianKey
	if keys == nil {
		var err error
		if keys, err = DebianKey(); err != nil {
			return DebianRelease{}, false, err
		}
	}
	dirURL := c.debianURL() + "/" + arch + "/iso-cd/"
	signedFile := filepath.Join(c.Dir, "debian-"+arch+".SHA512SUMS")
	sigFile := signedFile + ".sign"

	signed, netErr := c.get(ctx, dirURL+"SHA512SUMS")
	var sig []byte
	if netErr == nil {
		sig, netErr = c.get(ctx, dirURL+"SHA512SUMS.sign")
	}
	if netErr == nil {
		release, err := ParseSHA512SUMS(signed, sig, keys, arch)
		if err != nil {
			return DebianRelease{}, false, err
		}
		if err := os.MkdirAll(c.Dir, 0o755); err != nil {
			return DebianRelease{}, false, fmt.Errorf("create cache dir: %w", err)
		}
		if err := os.WriteFile(signedFile, signed, 0o644); err != nil {
			return DebianRelease{}, false, fmt.Errorf("store SHA512SUMS: %w", err)
		}
		if err := os.WriteFile(sigFile, sig, 0o644); err != nil {
			return DebianRelease{}, false, fmt.Errorf("store SHA512SUMS.sign: %w", err)
		}
		release.URL = dirURL + release.Name
		release.Size = c.headSize(ctx, release.URL)
		return release, false, nil
	}

	signed, err := os.ReadFile(signedFile)
	if err != nil {
		return DebianRelease{}, false, fmt.Errorf("debian release unavailable (%v) and no stored copy: %w", netErr, err)
	}
	sig, err = os.ReadFile(sigFile)
	if err != nil {
		return DebianRelease{}, false, fmt.Errorf("debian release unavailable (%v) and no stored signature: %w", netErr, err)
	}
	release, err := ParseSHA512SUMS(signed, sig, keys, arch)
	if err != nil {
		return DebianRelease{}, false, err
	}
	release.URL = dirURL + release.Name
	if fi, err := os.Stat(filepath.Join(c.Dir, imagesDir, release.Name)); err == nil && fi.Mode().IsRegular() {
		release.Size = fi.Size()
	}
	return release, true, nil
}

// headSize returns the Content-Length of rawURL from a HEAD request, or 0 when
// it is unknown.
func (c *Cache) headSize(ctx context.Context, rawURL string) int64 {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return 0
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength < 0 {
		return 0
	}
	return resp.ContentLength
}
