package bundlesource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return "", unreachable(err.Error())
	}
	resp, err := g.client().Do(req)
	if err != nil {
		return "", unreachable(err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", unreachable(fmt.Sprintf("HTTP %d", resp.StatusCode))
	}
	part := dest + ".part"
	out, err := os.Create(part)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.Remove(part)
		return "", unreachable(err.Error())
	}
	if err := out.Close(); err != nil {
		os.Remove(part)
		return "", err
	}
	if err := os.Rename(part, dest); err != nil {
		return "", err
	}
	return dest, nil
}
