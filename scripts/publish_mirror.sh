#!/usr/bin/env bash
#
# Assemble the public HACS mirror repo tree from this monorepo. The monorepo
# stays the single source of truth; the mirror is a derived artifact.
#
# Default run (no --release): sync the assembled tree into the mirror as a
# plain commit -- no tag, no CHANGELOG, no GitHub Release. --push then pushes
# the mirror branch.
#
# --release: regenerate integrations/homeassistant/CHANGELOG.md from history,
# commit + tag the mirror as vX.Y.Z, push branch + tag and start the mirror's
# release workflow, which cuts the GitHub Release (implies publishing -- needs
# `gh` authenticated as the mirror owner).
#
# --prerelease BRANCH: like --release, but publishes the next free beta
# vX.Y.Z-bN to the mirror branch BRANCH and marks the Release as a
# pre-release. The mirror's main stays untouched and no CHANGELOG is written.
set -euo pipefail

# Mirror checkout assembled into; determined from release.env (MIRROR_PATH).

usage() {
  cat >&2 <<EOF
publish_mirror.sh [--component NAME] [--mirror-path PATH] [--version X.Y.Z] [--dry-run] [--push] [--release | --prerelease BRANCH]

--component defaults to 'battery_soc'. Valid components have a mirror/COMPONENT/ directory.
--mirror-path defaults to the value in mirror/COMPONENT/release.env (MIRROR_PATH — the
  absolute path where the mirror repo is checked out).
--version defaults to the bare semver in the monorepo's
  custom_components/COMPONENT/manifest.json ("version" field), which the
  "Version bump" workflow patch-bumps on the PR branch. Pass it explicitly only
  to override that (first bootstrap release, or a manual major/minor jump).

Assembles the public HACS repo tree at PATH from this monorepo:
  1. scripts/vendor_core.py --check                     (abort on drift)
  2. rsync --delete custom_components/COMPONENT/  ->  PATH/custom_components/COMPONENT/
     render [%schema:...%] placeholders in its strings  (SCHEMA_DESCRIPTIONS in release.env)
  3. copy mirror/{hacs.json,README.md,info.md,LICENSE,AI-DISCLAIMER.md}  ->  PATH/
     copy mirror/.github                                ->  PATH/.github
     copy docs/img                                      ->  PATH/docs/img
     copy mirror/docs/*.md                              ->  PATH/docs/
  4. merge mirror/manifest.overrides.json over PATH/custom_components/COMPONENT/manifest.json
     (OWNER/REPO from mirror/release.env; version from --version)
  5. git -C PATH add -A && commit  (message and tag depend on --release)

--dry-run stops after step 4 and prints \`git status --porcelain\` + the
assembled manifest.json. Cannot be combined with --push or --release.

Without --release (default): step 5 commits the sync as "mirror: sync vX.Y.Z"
with NO tag, NO CHANGELOG, NO GitHub Release. --push then pushes the mirror
branch to \`origin\`. HACS keeps showing the last released version.

--release: regenerate integrations/homeassistant/CHANGELOG.md from history
(staged in the monorepo for you to commit), commit the mirror as
"release vX.Y.Z", tag vX.Y.Z, push branch + tag to \`origin\`, then start the
mirror's release workflow (.github/workflows/release.yml) with notes sliced
from that CHANGELOG.md and wait for it. The workflow creates the GitHub
Release only if no placeholder is left. Implies publishing (needs \`gh\`
authenticated as the mirror repo's owner); --push is redundant with it. Without a Release, HACS treats the repo as commit-based and shows
bare commit SHAs instead of the version.

--prerelease BRANCH: publish a beta for HACS "Show beta versions". Switches
the mirror checkout to BRANCH (taken from origin if it exists there, else
created from the current mirror HEAD; never main), sets the manifest version
to vX.Y.Z-bN with the next free N, commits "release vX.Y.Z-bN", tags it,
pushes branch + tag and starts the mirror's release workflow with
prerelease=true. X.Y.Z must not be released yet. The CHANGELOG is left alone.
The mirror checkout is switched back to its previous branch at the end. With
--dry-run it only prints the beta version it would publish.
EOF
  exit 2
}

component="battery_soc"
mirror_path=""
version=""
dry_run=0
push=0
release=0
prerelease_branch=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --component) component="${2:?--component needs a value}"; shift 2 ;;
    --mirror-path) mirror_path="${2:?--mirror-path needs a value}"; shift 2 ;;
    --version) version="${2:?--version needs a value}"; shift 2 ;;
    --dry-run) dry_run=1; shift ;;
    --push) push=1; shift ;;
    --release) release=1; shift ;;
    --prerelease) prerelease_branch="${2:?--prerelease needs a branch name}"; shift 2 ;;
    -h|--help) usage ;;
    *) echo "unknown argument: $1" >&2; usage ;;
  esac
done
if [[ "$dry_run" -eq 1 && ( "$push" -eq 1 || "$release" -eq 1 ) ]]; then
  echo "error: --dry-run cannot be combined with --push or --release." >&2
  exit 2
fi
if [[ -n "$prerelease_branch" && "$release" -eq 1 ]]; then
  echo "error: --prerelease cannot be combined with --release." >&2
  exit 2
fi
if [[ -n "$prerelease_branch" ]] && { [[ "$prerelease_branch" == "main" ]] \
    || ! git check-ref-format --branch "$prerelease_branch" >/dev/null 2>&1; }; then
  echo "error: --prerelease needs a valid branch name other than main (got '${prerelease_branch}')." >&2
  exit 2
fi

repo_root="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
ha="${repo_root}/integrations/homeassistant"
src_cc="${ha}/custom_components/${component}"
template="${ha}/mirror/${component}"

# Validate component and read mirror-specific settings from release.env
if [[ ! -f "${template}/release.env" ]]; then
  echo "error: mirror template for '${component}' not found at ${template}" >&2
  echo "Valid components:" >&2
  ls -1 "${ha}/mirror/" 2>/dev/null | grep -E '^[a-z_]+$' | sed 's/^/  /' >&2
  exit 1
fi
# shellcheck source=/dev/null
source "${template}/release.env"

# Default mirror_path to the value in release.env if not provided
if [[ -z "$mirror_path" ]]; then
  mirror_path="${MIRROR_PATH:?release.env must set MIRROR_PATH}"
fi

# Default the release version to the monorepo manifest's "version" field --
# the `Version bump` workflow keeps it current (scripts/version/components.sh
# COMPONENTS). It is a
# bare semver there (HACS/hassfest requirement); the script adds the "v"
# prefix itself for tags/commits/changelog.
if [[ -z "$version" ]]; then
  version="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("version",""))' \
    "${src_cc}/manifest.json")"
fi
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "error: no usable version (got '${version}'). Pass --version X.Y.Z or fix" >&2
  echo "       ${src_cc}/manifest.json's \"version\" field." >&2
  exit 2
fi

mkdir -p "$mirror_path"
mirror_path="$(cd "$mirror_path" && pwd)"

if ! git -C "$mirror_path" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "error: '$mirror_path' is not a git repo." >&2
  echo "Create it once via docs/integration/ha-integration-hacs-release.md." >&2
  exit 1
fi

# The version the mirror manifest, commit and tag carry: X.Y.Z, or the next
# free beta X.Y.Z-bN for --prerelease (tags fetched first so a beta cut
# elsewhere is not reused; offline is fine for a dry run).
release_version="$version"
if [[ -n "$prerelease_branch" ]]; then
  git -C "$mirror_path" fetch -q --tags origin 2>/dev/null || true
  if git -C "$mirror_path" rev-parse -q --verify "refs/tags/v${version}" >/dev/null; then
    echo "error: v${version} is already released; a beta needs a newer version." >&2
    echo "       Bump the manifest version or pass --version." >&2
    exit 1
  fi
  beta=1
  while git -C "$mirror_path" rev-parse -q --verify "refs/tags/v${version}-b${beta}" >/dev/null; do
    beta=$((beta + 1))
  done
  release_version="${version}-b${beta}"
fi

# Undone on exit: the release-notes temp file and, for --prerelease, the
# mirror checkout's switch to the pre-release branch.
notes_file=""
restore_ref=""
cleanup() {
  [[ -n "$notes_file" ]] && rm -f "$notes_file"
  if [[ -n "$restore_ref" ]] && ! git -C "$mirror_path" checkout -q "$restore_ref"; then
    echo "warning: could not switch ${mirror_path} back to ${restore_ref}." >&2
  fi
}
trap cleanup EXIT

# The pre-release branch: continue origin's copy if there is one, otherwise
# start it from the current mirror HEAD. The assembled tree is derived from
# the monorepo either way, so the start point only shapes the history.
if [[ -n "$prerelease_branch" && "$dry_run" -eq 0 ]]; then
  if [[ -n "$(git -C "$mirror_path" status --porcelain)" ]]; then
    echo "error: ${mirror_path} has uncommitted changes; commit or discard them first." >&2
    exit 1
  fi
  restore_ref="$(git -C "$mirror_path" symbolic-ref -q --short HEAD \
    || git -C "$mirror_path" rev-parse HEAD)"
  if git -C "$mirror_path" show-ref -q --verify "refs/heads/${prerelease_branch}"; then
    git -C "$mirror_path" checkout -q "$prerelease_branch"
    if git -C "$mirror_path" show-ref -q --verify "refs/remotes/origin/${prerelease_branch}"; then
      git -C "$mirror_path" merge -q --ff-only "origin/${prerelease_branch}"
    fi
  elif git -C "$mirror_path" show-ref -q --verify "refs/remotes/origin/${prerelease_branch}"; then
    git -C "$mirror_path" checkout -q -b "$prerelease_branch" --track "origin/${prerelease_branch}"
  else
    git -C "$mirror_path" checkout -q -b "$prerelease_branch"
  fi
fi

# 1. Never publish stale vendored artefacts.
python3 "${repo_root}/scripts/vendor_core.py" --check

# 1b. If this component requires icon checks, verify they pass.
if [[ "${ICON_CHECK:-0}" == "1" ]]; then
  python3 "${repo_root}/scripts/icons/flatten_icons.py" --check
fi

# 2. Integration source (drop caches).
mkdir -p "${mirror_path}/custom_components/${component}"
rsync -a --delete --exclude '__pycache__/' --exclude '*.pyc' \
  "${src_cc}/" "${mirror_path}/custom_components/${component}/"

# 2b. Shared field descriptions: the monorepo strings carry [%schema:...%]
# placeholders, the mirror ships the rendered text.
if [[ -n "${SCHEMA_DESCRIPTIONS:-}" ]]; then
  python3 "${repo_root}/scripts/render_ha_descriptions.py" \
    --render "${mirror_path}/custom_components/${component}" \
    --schema "${repo_root}/${SCHEMA_DESCRIPTIONS}"
fi

# 3. Repo-root files + workflows + README screenshots.
cp "${template}/hacs.json" "${template}/README.md" "${template}/info.md" \
   "${template}/LICENSE" "${template}/AI-DISCLAIMER.md" "${mirror_path}/"
rm -rf "${mirror_path}/.github"
cp -r "${template}/.github" "${mirror_path}/.github"
rm -rf "${mirror_path}/docs"
mkdir -p "${mirror_path}/docs"
# Optional: copy docs/img if specified in release.env (e.g. battery_soc includes screenshots)
if [[ -n "${DOCS_IMG:-}" ]]; then
  cp -r "${repo_root}/${DOCS_IMG}" "${mirror_path}/docs/img"
fi
# Optional: the per-icon SVGs the README's icon table links to (energy_node_icons)
if [[ -n "${DOCS_ICONS:-}" ]]; then
  cp -r "${repo_root}/${DOCS_ICONS}" "${mirror_path}/docs/icons"
fi
cp "${template}"/docs/*.md "${mirror_path}/docs/"

# 4. Rewrite the manifest's public fields (stdlib json, no jq).
OWNER="${OWNER:?release.env must set OWNER}" \
REPO="${REPO:?release.env must set REPO}" \
VERSION="$release_version" \
python3 - "${template}/manifest.overrides.json" \
          "${mirror_path}/custom_components/${component}/manifest.json" <<'PY'
import json, os, sys

overrides_path, manifest_path = sys.argv[1], sys.argv[2]
owner, repo, version = os.environ["OWNER"], os.environ["REPO"], os.environ["VERSION"]

with open(overrides_path) as fh:
    overrides = json.load(fh)
with open(manifest_path) as fh:
    manifest = json.load(fh)


def resolve(value):
    if isinstance(value, str):
        return value.replace("OWNER", owner).replace("REPO", repo)
    if isinstance(value, list):
        return [resolve(item) for item in value]
    return value


for key, value in overrides.items():
    manifest[key] = resolve(value)
manifest["version"] = version

with open(manifest_path, "w") as fh:
    json.dump(manifest, fh, indent=2)
    fh.write("\n")
PY

if [[ "$dry_run" -eq 1 ]]; then
  echo "--- dry run: assembled tree at ${mirror_path} ---"
  git -C "$mirror_path" status --porcelain
  echo "--- custom_components/${component}/manifest.json ---"
  cat "${mirror_path}/custom_components/${component}/manifest.json"
  if [[ -n "$prerelease_branch" ]]; then
    echo "--- pre-release: would publish v${release_version} to mirror branch ${prerelease_branch} ---"
  fi
  exit 0
fi

# 5a. Sync only (no --release): commit the assembled tree, optionally push the
# branch. No tag, no CHANGELOG regen, no GitHub Release -- HACS keeps showing
# the last released version until the next --release run.
if [[ "$release" -eq 0 && -z "$prerelease_branch" ]]; then
  if [[ -z "$(git -C "$mirror_path" status --porcelain)" ]]; then
    echo "mirror already in sync with the monorepo; nothing to commit."
  else
    git -C "$mirror_path" add -A
    git -C "$mirror_path" commit -q -m "mirror: sync v${version}"
    echo "committed mirror sync (v${version}, no tag) in ${mirror_path}"
  fi
  if [[ "$push" -eq 1 ]]; then
    branch="$(git -C "$mirror_path" rev-parse --abbrev-ref HEAD)"
    git -C "$mirror_path" push origin "$branch"
    echo "pushed ${branch}"
  else
    echo "not pushed. Re-run with --push to push the mirror branch, or with --release to cut a release." >&2
  fi
  exit 0
fi

# 5b. Release (--release). First rebuild the component's CHANGELOG.md
# from the monorepo history and stage it there -- it is a monorepo-tracked
# file, so this script only stages it; you commit it alongside the version
# bump. A pre-release leaves the CHANGELOG alone.
CHANGELOG_TARGET="${CHANGELOG_TARGET:?release.env must set CHANGELOG_TARGET}" \
CHANGELOG_PATH="${CHANGELOG_PATH:?release.env must set CHANGELOG_PATH}"
if [[ -z "$prerelease_branch" ]]; then
  "${repo_root}/scripts/generate_changelog.sh" "$CHANGELOG_TARGET"
  git -C "$repo_root" add "$CHANGELOG_PATH"
  if git -C "$repo_root" diff --cached --quiet -- "$CHANGELOG_PATH"; then
    echo "${CHANGELOG_PATH} unchanged."
  else
    echo "note: ${CHANGELOG_PATH} regenerated and staged in the monorepo -- commit it with the release."
  fi
fi

# 6. Commit + tag the mirror, then publish: push branch + tag and let the
# mirror's release workflow cut the GitHub Release. HACS only leaves commit mode (bare SHAs, dead "release
# announcement" link) once a Release exists.
if git -C "$mirror_path" rev-parse -q --verify "refs/tags/v${release_version}" >/dev/null; then
  echo "error: tag v${release_version} already exists in ${mirror_path}." >&2
  echo "       Bump the manifest version (commit it) or pass --version." >&2
  exit 1
fi
if [[ -n "$(git -C "$mirror_path" status --porcelain)" ]]; then
  git -C "$mirror_path" add -A
  git -C "$mirror_path" commit -q -m "release v${release_version}"
fi
git -C "$mirror_path" tag "v${release_version}"
echo "committed and tagged v${release_version} in ${mirror_path}"

branch="$(git -C "$mirror_path" rev-parse --abbrev-ref HEAD)"
git -C "$mirror_path" push origin "$branch"
git -C "$mirror_path" push origin "v${release_version}"

# Release notes: the "## vX.Y.Z ..." section of the changelog, up to the next
# "## " heading. Falls back to a one-liner if that version has no section yet.
# A pre-release has no changelog section; its notes name the monorepo source.
notes_file="$(mktemp)"
RELEASE_NAME="${RELEASE_NAME:?release.env must set RELEASE_NAME}"
if [[ -n "$prerelease_branch" ]]; then
  source_branch="${GITHUB_REF_NAME:-$(git -C "$repo_root" rev-parse --abbrev-ref HEAD)}"
  printf 'Pre-release %s of the %s, built from energy-node %s (%s).\n' \
    "v${release_version}" "$RELEASE_NAME" "$source_branch" \
    "$(git -C "$repo_root" rev-parse --short HEAD)" > "$notes_file"
else
  awk -v ver="v${release_version}" '
    $0 ~ "^## " ver "( |$|\\()" { grab = 1; next }
    grab && /^## / { exit }
    grab { print }
  ' "${repo_root}/${CHANGELOG_PATH}" > "$notes_file"
  if [[ ! -s "$notes_file" ]]; then
    printf 'Release %s of the %s.\n' "v${release_version}" "$RELEASE_NAME" > "$notes_file"
  fi
fi

# The release workflow checks the tagged tree for unrendered placeholders
# before it creates the Release, so nothing half-rendered gets published.
dispatched_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
gh workflow run release.yml \
  --repo "${OWNER}/${REPO}" \
  --ref "$branch" \
  -f tag="v${release_version}" \
  -f notes="$(cat "$notes_file")" \
  -f prerelease="$([[ -n "$prerelease_branch" ]] && echo true || echo false)"
run_id=""
for _ in $(seq 1 30); do
  run_id="$(gh run list --repo "${OWNER}/${REPO}" --workflow release.yml \
    --event workflow_dispatch --created ">=${dispatched_at}" \
    --json databaseId --jq '.[0].databaseId // empty')"
  [[ -n "$run_id" ]] && break
  sleep 2
done
if [[ -z "$run_id" ]]; then
  echo "error: started the release workflow in ${OWNER}/${REPO} but could not find its run." >&2
  echo "       Check https://github.com/${OWNER}/${REPO}/actions -- tag v${release_version} is pushed." >&2
  exit 1
fi
gh run watch "$run_id" --repo "${OWNER}/${REPO}" --exit-status
echo "pushed ${branch} + v${release_version}; the release workflow created the GitHub Release"
