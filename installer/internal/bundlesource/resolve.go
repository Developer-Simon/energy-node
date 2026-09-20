package bundlesource

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Developer-Simon/energy-node-installer/internal/bundle"
)

// Kind is the package source the operator chose.
type Kind string

const (
	KindBundled Kind = "bundled"
	KindFile    Kind = "file"
	KindRepo    Kind = "repo"
	KindGitHub  Kind = "github"
)

// Request is one resolution: which source, for which node architecture.
type Request struct {
	Kind       Kind
	Path       string
	Arch       string
	User, Base string
	Log        func(line string)
}

// Resolved is a verified, unpacked bundle.
type Resolved struct {
	Dir         string
	ArchivePath string
	Manifest    *bundle.Manifest
	Signed      bool
	Cleanup     func()
}

// Resolver turns a Request into a Resolved bundle.
type Resolver struct {
	BundledDir string
	WorkDir    string
	GitHub     *GitHub
	PublicKey  ed25519.PublicKey
	Build      func(ctx context.Context, args bundle.BuildArgs) (string, error)
}

func noop() {}

// Resolve verifies and unpacks the chosen source. On error nothing is left
// behind in WorkDir.
func (r *Resolver) Resolve(ctx context.Context, req Request) (*Resolved, error) {
	log := req.Log
	if log == nil {
		log = func(string) {}
	}
	if req.Kind == KindBundled {
		return r.finish(req, r.BundledDir, "", false, noop, "")
	}

	if err := os.MkdirAll(r.WorkDir, 0o755); err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp(r.WorkDir, "resolve-*")
	if err != nil {
		return nil, err
	}
	cleanup := func() { os.RemoveAll(work) }

	var archive string
	// cached is set when archive lives in the download cache, so a failed
	// verification can drop it.
	cached := ""
	switch req.Kind {
	case KindFile:
		archive = req.Path
	case KindRepo:
		archive, err = r.buildFromRepo(ctx, req, log)
	case KindGitHub:
		if r.GitHub == nil {
			err = &Error{Code: CodeGitHubUnreachable, Detail: "not configured"}
		} else {
			archive, err = r.GitHub.Fetch(ctx, req.Arch, log)
			cached = archive
		}
	default:
		err = fmt.Errorf("unknown package source %q", req.Kind)
	}
	if err != nil {
		cleanup()
		return nil, err
	}

	log("Paket entpacken")
	dir := filepath.Join(work, "bundle")
	if err := bundle.ExtractArchive(archive, dir); err != nil {
		cleanup()
		if cached != "" {
			os.Remove(cached)
		}
		return nil, &Error{Code: CodePackageFileInvalid, Detail: err.Error()}
	}
	return r.finish(req, dir, archive, req.Kind == KindGitHub, cleanup, cached)
}

// finish verifies dir and checks its architecture. cleanup and cached are
// released on failure.
func (r *Resolver) finish(req Request, dir, archive string, strict bool, cleanup func(), cached string) (*Resolved, error) {
	fail := func(err error) (*Resolved, error) {
		cleanup()
		if cached != "" {
			os.Remove(cached)
		}
		return nil, err
	}

	var manifest *bundle.Manifest
	var err error
	signed := false
	if strict || bundle.HasSignature(dir) {
		manifest, err = bundle.Verify(dir, r.PublicKey)
		signed = err == nil
	} else {
		manifest, err = bundle.VerifyDev(dir)
	}
	if err != nil {
		return fail(err)
	}
	if manifest.Arch != req.Arch {
		return fail(&bundle.Error{
			Code:    bundle.FaultArchMismatch,
			Message: fmt.Sprintf("bundle is built for %s, the node needs %s", manifest.Arch, req.Arch),
		})
	}
	return &Resolved{Dir: dir, ArchivePath: archive, Manifest: manifest, Signed: signed, Cleanup: cleanup}, nil
}

func (r *Resolver) buildFromRepo(ctx context.Context, req Request, log func(string)) (string, error) {
	if err := CheckRepo(req.Path); err != nil {
		return "", err
	}
	cacheRoot := os.TempDir()
	if r.GitHub != nil && r.GitHub.CacheDir != "" {
		cacheRoot = r.GitHub.CacheDir
	}
	outDir := filepath.Join(cacheRoot, "repo-build-"+req.Arch)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	// make_bundle.sh keeps its wheel cache in outDir/cache; only the
	// archives are removed, because BuildViaRepo refuses to choose between
	// several.
	old, _ := filepath.Glob(filepath.Join(outDir, "energy-node-*.tar.gz"))
	for _, f := range old {
		os.Remove(f)
	}

	build := r.Build
	if build == nil {
		build = bundle.BuildViaRepo
	}
	log("Bundle aus dem Repository bauen (das kann einige Minuten dauern)")
	archive, err := build(ctx, bundle.BuildArgs{
		RepoRoot: req.Path, Arch: req.Arch, User: req.User, Base: req.Base,
		OutDir: outDir, Log: log,
	})
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", &Error{Code: CodeBuildFailed, Detail: err.Error()}
	}
	return archive, nil
}
