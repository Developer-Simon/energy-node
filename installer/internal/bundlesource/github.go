package bundlesource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// downloadTimeout bounds one download, a bundle archive or a pinned file.
	downloadTimeout = 15 * time.Minute
	// maxBundleBytes caps a bundle archive, maxPinBytes a pinned .deb or wheel.
	maxBundleBytes = 1 << 30
	maxPinBytes    = 256 << 20
)

// errTransfer marks a download that did not produce a complete file: a
// network error, a status other than 200 or a body over the size cap. It is
// a failure to fetch, not a verdict on the file.
type errTransfer struct{ detail string }

func (e *errTransfer) Error() string { return e.detail }

// fetchTo streams url into dest through dest.part and returns the hex
// SHA-256 of what was written. A redirect refused by client's CheckRedirect
// surfaces as *Error; every other failure to get the file is an *errTransfer.
func fetchTo(ctx context.Context, client *http.Client, url, dest string, maxBytes int64) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", &errTransfer{err.Error()}
	}
	resp, err := client.Do(req)
	if err != nil {
		var refused *Error
		if errors.As(err, &refused) {
			return "", refused
		}
		return "", &errTransfer{err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", &errTransfer{fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	part := dest + ".part"
	out, err := os.Create(part)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(out, hash), io.LimitReader(resp.Body, maxBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		os.Remove(part)
		return "", &errTransfer{copyErr.Error()}
	}
	if n > maxBytes {
		os.Remove(part)
		return "", &errTransfer{fmt.Sprintf("body larger than %d bytes", maxBytes)}
	}
	if closeErr != nil {
		os.Remove(part)
		return "", closeErr
	}
	if err := os.Rename(part, dest); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

const (
	defaultAPIBase = "https://api.github.com"
	defaultRepo    = "Developer-Simon/energy-node"
)

// GitHub finds and downloads the signed bundle a release carries.
type GitHub struct {
	APIBase  string
	Repo     string
	CacheDir string
	HTTP     *http.Client
}

// Asset is one downloadable bundle.
type Asset struct{ Tag, Name, URL string }

type releaseJSON struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func (g *GitHub) client() *http.Client {
	if g.HTTP != nil {
		return g.HTTP
	}
	return http.DefaultClient
}

func (g *GitHub) apiBase() string {
	if g.APIBase != "" {
		return strings.TrimSuffix(g.APIBase, "/")
	}
	return defaultAPIBase
}

func (g *GitHub) repo() string {
	if g.Repo != "" {
		return g.Repo
	}
	return defaultRepo
}

func unreachable(detail string) *Error {
	return &Error{Code: CodeGitHubUnreachable, Detail: detail}
}

// FindAsset returns the bundle asset of the newest stable release that has
// one for arch.
func (g *GitHub) FindAsset(ctx context.Context, arch string) (Asset, error) {
	url := fmt.Sprintf("%s/repos/%s/releases?per_page=30", g.apiBase(), g.repo())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Asset{}, unreachable(err.Error())
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := g.client().Do(req)
	if err != nil {
		return Asset{}, unreachable(err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Asset{}, unreachable(fmt.Sprintf("HTTP %d", resp.StatusCode))
	}
	var releases []releaseJSON
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return Asset{}, unreachable(err.Error())
	}
	suffix := "-" + arch + ".tar.gz"
	for _, release := range releases {
		if release.Draft || release.Prerelease {
			continue
		}
		for _, asset := range release.Assets {
			if strings.HasPrefix(asset.Name, "energy-node-") && strings.HasSuffix(asset.Name, suffix) {
				return Asset{Tag: release.TagName, Name: asset.Name, URL: asset.URL}, nil
			}
		}
	}
	return Asset{}, &Error{Code: CodeGitHubNoRelease, Detail: arch}
}

// Fetch makes sure the newest matching bundle archive is in CacheDir and
// returns its path. A cached file is reused as is; the caller verifies it
// and removes it if verification fails.
func (g *GitHub) Fetch(ctx context.Context, arch string, note func(key string, args map[string]string)) (string, error) {
	if note == nil {
		note = func(string, map[string]string) {}
	}
	note("package.log.github_search", map[string]string{"arch": arch})
	asset, err := g.FindAsset(ctx, arch)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(g.CacheDir, asset.Tag, asset.Name)
	if _, err := os.Stat(dest); err == nil {
		note("package.log.cached", map[string]string{"name": asset.Name})
		return dest, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	note("package.log.download", map[string]string{"name": asset.Name})
	if _, err := fetchTo(ctx, g.client(), asset.URL, dest, maxBundleBytes); err != nil {
		var transfer *errTransfer
		if errors.As(err, &transfer) {
			return "", unreachable(transfer.detail)
		}
		return "", err
	}
	return dest, nil
}
