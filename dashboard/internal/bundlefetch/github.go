package bundlefetch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const (
	defaultAPIBase = "https://api.github.com"
	assetPrefix    = "energy-node-"
	// maxListBytes bounds the releases listing we are willing to parse.
	maxListBytes = 4 << 20
)

// maxArchiveBytes caps one download. A real bundle (wheels, tailscale, the
// dashboard binary) is well below 100 MB; the cap stops a wrong or hostile
// response from filling the SD card. A variable so tests can lower it.
var maxArchiveBytes int64 = 512 << 20

// Asset is one downloadable bundle.
type Asset struct {
	Tag     string
	Version string // "v0.7.0", equal to the bundle manifest's version
	Name    string
	URL     string
	Size    int64
}

// Client talks to the GitHub releases API of one repository.
type Client struct {
	APIBase string       // default "https://api.github.com"
	Repo    string       // "owner/name"
	HTTP    *http.Client // default http.DefaultClient
}

type releaseJSON struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) apiBase() string {
	if c.APIBase != "" {
		return strings.TrimSuffix(c.APIBase, "/")
	}
	return defaultAPIBase
}

// FindAsset returns the bundle asset of the newest stable release that has
// one for arch. Only assets named energy-node-v<version>-<arch>.tar.gz count:
// the repository also publishes energy-node-dashboard_* and installer
// archives that are not bundles.
func (c *Client) FindAsset(ctx context.Context, arch string) (Asset, error) {
	url := fmt.Sprintf("%s/repos/%s/releases?per_page=30", c.apiBase(), c.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Asset{}, unreachable(err.Error())
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.client().Do(req)
	if err != nil {
		return Asset{}, unreachable(err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Asset{}, unreachable(fmt.Sprintf("HTTP %d", resp.StatusCode))
	}
	var releases []releaseJSON
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxListBytes)).Decode(&releases); err != nil {
		return Asset{}, unreachable(err.Error())
	}
	suffix := "-" + arch + ".tar.gz"
	for _, release := range releases {
		if release.Draft || release.Prerelease {
			continue
		}
		for _, asset := range release.Assets {
			if !strings.HasPrefix(asset.Name, assetPrefix+"v") || !strings.HasSuffix(asset.Name, suffix) {
				continue
			}
			version := strings.TrimSuffix(strings.TrimPrefix(asset.Name, assetPrefix), suffix)
			return Asset{Tag: release.TagName, Version: version, Name: asset.Name, URL: asset.URL, Size: asset.Size}, nil
		}
	}
	return Asset{}, &Error{Code: CodeGitHubNoRelease, Detail: arch}
}

// progress logs a line at every 20 % of a download of known size.
type progress struct {
	size, seen, next int64
	log              func(string)
}

func (p *progress) Write(b []byte) (int, error) {
	p.seen += int64(len(b))
	for p.size > 0 && p.next <= 100 && p.seen*100 >= p.size*p.next {
		p.log(fmt.Sprintf("%d %% geladen", p.next))
		p.next += 20
	}
	return len(b), nil
}

// Download streams asset to dest, refusing more than maxArchiveBytes.
func (c *Client) Download(ctx context.Context, asset Asset, dest string, log func(string)) error {
	if log == nil {
		log = func(string) {}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return unreachable(err.Error())
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return unreachable(err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return unreachable(fmt.Sprintf("HTTP %d", resp.StatusCode))
	}
	if resp.ContentLength > maxArchiveBytes {
		return tooLarge(fmt.Sprintf("%d bytes", resp.ContentLength))
	}
	out, err := os.Create(dest)
	if err != nil {
		return installFailed(err)
	}
	prog := &progress{size: resp.ContentLength, next: 20, log: log}
	n, err := io.Copy(io.MultiWriter(out, prog), io.LimitReader(resp.Body, maxArchiveBytes+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return unreachable(err.Error())
	}
	if n > maxArchiveBytes {
		return tooLarge(fmt.Sprintf("more than %d bytes", maxArchiveBytes))
	}
	return nil
}
