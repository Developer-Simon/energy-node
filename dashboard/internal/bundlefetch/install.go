package bundlefetch

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// validate checks the parts of an unpacked bundle the dashboard can judge
// without a key: a readable manifest.json with a version, the expected
// architecture, and the presence of manifest.json.sig. Whether the signature
// is *valid* is the updater's job. It returns the bundle's version.
func validate(dir, arch string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return "", invalid("manifest.json fehlt")
	}
	var head struct {
		Version string `json:"version"`
		Arch    string `json:"arch"`
	}
	if err := json.Unmarshal(raw, &head); err != nil || head.Version == "" {
		return "", invalid("manifest.json unlesbar")
	}
	if head.Arch != arch {
		return "", &Error{Code: CodeArchMismatch, Detail: head.Arch + " != " + arch}
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.json.sig")); err != nil {
		return "", &Error{Code: CodeBundleUnsigned}
	}
	return head.Version, nil
}

// candidateVersion is the version of the bundle already sitting in dir, or
// "" when there is none (or it is not usable for arch).
func candidateVersion(dir, arch string) string {
	version, err := validate(dir, arch)
	if err != nil {
		return ""
	}
	return version
}

// swapIn makes staged the new dest. Both must be on the same filesystem. The
// previous dest is moved aside first and restored if the final rename fails,
// so a failure never leaves the candidate half-replaced.
func swapIn(staged, dest string) error {
	old := dest + ".old"
	if err := os.RemoveAll(old); err != nil {
		return installFailed(err)
	}
	hadOld := true
	if err := os.Rename(dest, old); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return installFailed(err)
		}
		hadOld = false
	}
	if err := os.Rename(staged, dest); err != nil {
		if hadOld {
			_ = os.Rename(old, dest)
		}
		return installFailed(err)
	}
	_ = os.RemoveAll(old)
	return nil
}
