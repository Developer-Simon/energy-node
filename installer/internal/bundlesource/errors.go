// Package bundlesource turns "where the package comes from" (the bundled
// directory, a picked archive, a repo checkout, a GitHub release) plus the
// node's architecture into a verified, unpacked bundle. It knows nothing
// about SSH or the web UI; internal/host stages the result on the node.
package bundlesource

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
	CodePackageFileInvalid = "PACKAGE_FILE_INVALID"
	CodeRepoNotACheckout   = "REPO_NOT_A_CHECKOUT"
	CodeBuildToolsMissing  = "BUILD_TOOLS_MISSING"
	CodeBuildFailed        = "BUILD_FAILED"
	CodeGitHubUnreachable  = "GITHUB_UNREACHABLE"
	CodeGitHubNoRelease    = "GITHUB_NO_RELEASE"
)
