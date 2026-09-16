// Package updatecheck answers one question: is there a newer energy-node
// release on GitHub than the one currently running? It never downloads or
// stages anything -- getting a new bundle onto the node is a separate,
// larger piece of work (see docs/knowledge/dashboard/updater-job-protocol.md,
// "What an OTA delivery still has to add"). This package only produces the
// version comparison a badge or an on-demand button can show.
package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Result is the outcome of a single check.
type Result struct {
	Current     string    `json:"current"`
	Latest      string    `json:"latest"`
	Available   bool      `json:"available"`
	PublishedAt time.Time `json:"published_at,omitempty"`
	NotesURL    string    `json:"notes_url,omitempty"`
	CheckedAt   time.Time `json:"checked_at"`
}

// Checker asks the GitHub Releases API for the newest published (non-draft,
// non-prerelease -- /releases/latest already excludes both) release.
type Checker struct {
	// Repo is "owner/name", e.g. "Developer-Simon/energy-node".
	Repo string
	// HTTPClient defaults to http.DefaultClient when nil.
	HTTPClient *http.Client
	// BaseURL overrides the GitHub API host; empty means the real API.
	// Tests point this at an httptest.Server.
	BaseURL string
	// Now defaults to time.Now when nil.
	Now func() time.Time
}

type githubRelease struct {
	TagName     string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
}

// The optional "v" matters: main.buildVersion is built from dashboard/VERSION
// (which itself is "vX.Y.Z") plus an optional "-dev" or "-branch.N" suffix,
// so a real build's current version always carries the prefix.
var versionLike = regexp.MustCompile(`^v?\d+(\.\d+){0,2}`)

// Check compares current (typically main.buildVersion) against the latest
// GitHub release tag. A current version that does not look like a version
// number -- "dev" builds outside of a release -- never reports an update,
// since there is nothing meaningful to compare against.
func (c *Checker) Check(ctx context.Context, current string) (Result, error) {
	now := c.Now
	if now == nil {
		now = time.Now
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	base := c.BaseURL
	if base == "" {
		base = "https://api.github.com"
	}

	url := fmt.Sprintf("%s/repos/%s/releases/latest", base, c.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("updatecheck: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("updatecheck: unexpected status %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return Result{}, fmt.Errorf("updatecheck: decoding release: %w", err)
	}

	latest := strings.TrimPrefix(strings.TrimSpace(release.TagName), "v")
	result := Result{
		Current:     current,
		Latest:      latest,
		PublishedAt: release.PublishedAt,
		NotesURL:    release.HTMLURL,
		CheckedAt:   now().UTC(),
	}
	if versionLike.MatchString(current) {
		result.Available = compareVersions(latest, current) > 0
	}
	return result, nil
}

// compareVersions compares two dotted version strings by their first three
// numeric components (major.minor.patch), tolerant of missing components
// and of a non-numeric suffix on a component (e.g. "3-rc1"). It returns -1,
// 0 or 1, the same convention as strings.Compare.
func compareVersions(a, b string) int {
	pa, pb := versionParts(a), versionParts(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionParts(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	fields := strings.SplitN(v, ".", 3)
	var out [3]int
	for i := 0; i < len(fields) && i < 3; i++ {
		out[i] = leadingInt(fields[i])
	}
	return out
}

func leadingInt(s string) int {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	n, _ := strconv.Atoi(s[:end])
	return n
}

// Cache holds the most recent Result in memory, shared between the
// on-demand check endpoint and the periodic background check. It resets on
// restart -- deliberately: the background loop re-checks immediately when
// it finds no or a stale cached result (see main.go), so a restart costs at
// most one extra GitHub request rather than needing a file on disk.
type Cache struct {
	mu     sync.RWMutex
	result Result
	has    bool
}

// Get returns the cached result and whether one exists yet.
func (c *Cache) Get() (Result, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.result, c.has
}

// Set stores a new result, replacing any previous one.
func (c *Cache) Set(r Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.result = r
	c.has = true
}
