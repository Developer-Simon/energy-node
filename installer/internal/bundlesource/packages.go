package bundlesource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
)

// LocalPackage is one pinned file that is in the cache and matches its
// SHA-256 pin. Name is the package key for a .deb and the file name for a
// wheel.
type LocalPackage struct {
	Name, File, SHA256, Path string
}

// maxParallelDownloads bounds the pinned downloads that run at once.
const maxParallelDownloads = 4

// maxRedirects caps the redirects one pinned download may follow.
const maxRedirects = 5

// Hosts a pin may name. A GitHub release download answers with a redirect
// to a release-asset host, see redirectPolicy.
var (
	allowedDebHosts   = []string{"github.com", "pkgs.tailscale.com"}
	allowedWheelHosts = []string{"files.pythonhosted.org", "www.piwheels.org", "archive1.piwheels.org"}
)

// packageRules is the host and scheme policy for pinned downloads.
// Resolver.rules overrides the default, which lets tests point the policy
// at httptest servers over plain HTTP.
type packageRules struct {
	debHosts, wheelHosts []string
	scheme               string
}

var defaultPackageRules = packageRules{
	debHosts:   allowedDebHosts,
	wheelHosts: allowedWheelHosts,
	scheme:     "https",
}

// pinJob is one file to fetch: a .deb or a wheel from the manifest.
type pinJob struct {
	wheel  bool
	name   string
	file   string
	sha    string
	url    string
	origin string
	hosts  []string
	path   string
	err    error
}

// fetchPins fills res with the pinned .deb packages and wheels of m. Files
// already in the cache with the right checksum are not fetched again, and a
// wheel shipped inside the bundle is left alone. A download that does not
// get a file goes into the Missing lists; a wrong checksum or a refused URL
// is an error.
func (r *Resolver) fetchPins(ctx context.Context, req Request, dir string, m *bundle.Manifest, res *Resolved) error {
	if len(m.Packages) == 0 && len(m.Wheels) == 0 {
		return nil
	}
	rules := defaultPackageRules
	if r.rules != nil {
		rules = *r.rules
	}
	note := req.Note
	if note == nil {
		note = func(string, map[string]string) {}
	}
	cacheRoot := r.CacheDir
	if cacheRoot == "" {
		cacheRoot = r.WorkDir
	}

	jobs, err := pinJobs(rules, dir, m)
	if err != nil {
		return err
	}
	var pending []*pinJob
	for _, j := range jobs {
		j.path = filepath.Join(cacheRoot, "packages", j.sha, j.file)
		if sum, err := hashFile(j.path); err == nil {
			if sum == j.sha {
				note("package.log.cached", map[string]string{"name": j.name})
				continue
			}
			os.Remove(j.path)
		}
		if err := os.MkdirAll(filepath.Dir(j.path), 0o755); err != nil {
			return err
		}
		note("package.log.download_package", map[string]string{"name": j.name})
		pending = append(pending, j)
	}

	base := r.pinHTTPClient()
	sem := make(chan struct{}, maxParallelDownloads)
	var wg sync.WaitGroup
	for _, j := range pending {
		wg.Add(1)
		go func(j *pinJob) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			j.err = downloadPin(ctx, base, rules, j)
		}(j)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}

	for _, j := range jobs {
		if j.err != nil {
			var transfer *errTransfer
			if !errors.As(j.err, &transfer) {
				return j.err
			}
			if j.wheel {
				res.MissingWheels = append(res.MissingWheels, j.name)
			} else {
				res.MissingPackages = append(res.MissingPackages, j.name)
			}
			continue
		}
		lp := LocalPackage{Name: j.name, File: j.file, SHA256: j.sha, Path: j.path}
		if j.wheel {
			res.Wheels = append(res.Wheels, lp)
		} else {
			res.Packages = append(res.Packages, lp)
		}
	}
	return nil
}

// pinJobs lists the files to fetch, packages first in key order, and checks
// every name, checksum and URL before anything is downloaded.
func pinJobs(rules packageRules, dir string, m *bundle.Manifest) ([]*pinJob, error) {
	var jobs []*pinJob
	names := make([]string, 0, len(m.Packages))
	for name := range m.Packages {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		pin := m.Packages[name]
		jobs = append(jobs, &pinJob{name: name, file: pin.File, sha: pin.SHA256, url: pin.URL, hosts: rules.debHosts})
	}
	for _, w := range m.Wheels {
		if validFileName(w.File) && isFile(filepath.Join(dir, "wheels", w.File)) {
			continue
		}
		jobs = append(jobs, &pinJob{wheel: true, name: w.File, file: w.File, sha: w.SHA256, url: w.URL, hosts: rules.wheelHosts})
	}
	for _, j := range jobs {
		if !validFileName(j.file) {
			return nil, &Error{Code: CodePackageDownloadInvalid, Detail: j.file}
		}
		sha := strings.ToLower(j.sha)
		if raw, err := hex.DecodeString(sha); err != nil || len(raw) != sha256.Size {
			return nil, &Error{Code: CodePackageDownloadInvalid, Detail: j.file}
		}
		j.sha = sha
		u, err := checkPinURL(rules, j.url, j.hosts)
		if err != nil {
			return nil, err
		}
		j.origin = strings.ToLower(u.Hostname())
	}
	return jobs, nil
}

// checkPinURL accepts a pin URL only with the allowed scheme and an allowed
// host.
func checkPinURL(rules packageRules, raw string, allowed []string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != rules.scheme || !hostAllowed(allowed, u.Hostname()) {
		return nil, &Error{Code: CodePackageURLRejected, Detail: raw}
	}
	return u, nil
}

// redirectPolicy follows redirects only to the same scheme and to an allowed
// host. A pin on github.com may also be redirected to GitHub's release-asset
// hosts (githubusercontent.com and github.com subdomains), which is where
// GitHub serves release downloads.
func redirectPolicy(rules packageRules, allowed []string, origin string) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return &errTransfer{"too many redirects"}
		}
		host := strings.ToLower(req.URL.Hostname())
		if req.URL.Scheme == rules.scheme && hostAllowed(allowed, host) {
			return nil
		}
		if req.URL.Scheme == rules.scheme && origin == "github.com" && isGitHubAssetHost(host) {
			return nil
		}
		return &Error{Code: CodePackageURLRejected, Detail: host}
	}
}

func isGitHubAssetHost(host string) bool {
	for _, domain := range []string{"github.com", "githubusercontent.com"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func hostAllowed(allowed []string, host string) bool {
	for _, a := range allowed {
		if strings.EqualFold(a, host) {
			return true
		}
	}
	return false
}

// downloadPin fetches one file into its cache path and checks its SHA-256.
// A mismatch removes the file and is a PACKAGE_DOWNLOAD_INVALID error.
func downloadPin(ctx context.Context, base *http.Client, rules packageRules, j *pinJob) error {
	client := *base
	client.CheckRedirect = redirectPolicy(rules, j.hosts, j.origin)
	sum, err := fetchTo(ctx, &client, j.url, j.path, maxPinBytes)
	if err != nil {
		return err
	}
	if sum != j.sha {
		os.Remove(j.path)
		return &Error{Code: CodePackageDownloadInvalid, Detail: j.file}
	}
	return nil
}

func (r *Resolver) pinHTTPClient() *http.Client {
	if r.GitHub != nil {
		return r.GitHub.client()
	}
	return http.DefaultClient
}

// validFileName rejects names that would leave the cache directory or the
// wheels directory.
func validFileName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`)
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
