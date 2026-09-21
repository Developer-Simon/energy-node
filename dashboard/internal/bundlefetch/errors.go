// Package bundlefetch downloads the newest signed release bundle for this
// node's architecture into the candidate directory the dashboard's redeploy
// host stages from. It never verifies the signature: that happens only in
// energy-node-updater.sh, as root, after the bundle has left the
// dashboard-writable directory (docs/knowledge/dashboard/updater-job-protocol.md).
// What it does check is structural, so a wrong or damaged download fails
// early with a readable error instead of a rejected job.
package bundlefetch

// Error carries a language-neutral code the UI translates (error.<Code>).
type Error struct {
	Code   string
	Detail string
}

func (e *Error) Error() string {
	if e.Detail == "" {
		return e.Code
	}
	return e.Code + ": " + e.Detail
}

const (
	CodeGitHubUnreachable = "GITHUB_UNREACHABLE"
	CodeGitHubNoRelease   = "GITHUB_NO_RELEASE"
	CodeBundleTooLarge    = "BUNDLE_TOO_LARGE"
	CodeBundleInvalid     = "PACKAGE_FILE_INVALID"
	CodeBundleUnsigned    = "BUNDLE_UNSIGNED"
	CodeArchMismatch      = "ARCH_MISMATCH"
	CodeInstallFailed     = "CANDIDATE_INSTALL_FAILED"
)

func unreachable(detail string) *Error { return &Error{Code: CodeGitHubUnreachable, Detail: detail} }
func invalid(detail string) *Error     { return &Error{Code: CodeBundleInvalid, Detail: detail} }
func tooLarge(detail string) *Error    { return &Error{Code: CodeBundleTooLarge, Detail: detail} }
func installFailed(err error) *Error   { return &Error{Code: CodeInstallFailed, Detail: err.Error()} }
