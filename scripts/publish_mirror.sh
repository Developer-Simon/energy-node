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
# commit + tag the mirror as vX.Y.Z, push branch + tag and cut the GitHub
# Release (implies publishing -- needs `gh` authenticated as the mirror owner).
set -euo pipefail

# Local mirror checkout assembled into by default; override with --mirror-path.
DEFAULT_MIRROR_PATH="/home/simon/dev/ha-battery-soc"

usage() {
  cat >&2 <<EOF
publish_mirror.sh [--mirror-path PATH] [--version X.Y.Z] [--dry-run] [--push] [--release]

--mirror-path defaults to ${DEFAULT_MIRROR_PATH}.
--version defaults to the bare semver in the monorepo's
  custom_components/battery_soc/manifest.json ("version" field), which the
  pre-commit hook auto-bumps. Pass it explicitly only to override that
  (first bootstrap release, or a manual major/minor jump).

Assembles the public HACS repo tree at PATH from this monorepo:
  1. scripts/vendor_core.py --check                     (abort on drift)
  2. rsync --delete custom_components/battery_soc/  ->  PATH/custom_components/battery_soc/
  3. copy mirror/{hacs.json,README.md,info.md,LICENSE,AI-DISCLAIMER.md}  ->  PATH/
     copy mirror/.github                                ->  PATH/.github
     copy docs/img                                      ->  PATH/docs/img
     copy mirror/docs/*.md                              ->  PATH/docs/
  4. merge mirror/manifest.overrides.json over PATH/custom_components/battery_soc/manifest.json
     (OWNER/REPO from mirror/release.env; version from --version)
  5. git -C PATH add -A && commit  (message and tag depend on --release)

--dry-run stops after step 4 and prints \`git status --porcelain\` + the
assembled manifest.json. Cannot be combined with --push or --release.

Without --release (default): step 5 commits the sync as "mirror: sync vX.Y.Z"
with NO tag, NO CHANGELOG, NO GitHub Release. --push then pushes the mirror
branch to \`origin\`. HACS keeps showing the last released version.

--release: regenerate integrations/homeassistant/CHANGELOG.md from history
(staged in the monorepo for you to commit), commit the mirror as
"release vX.Y.Z", tag vX.Y.Z, push branch + tag to \`origin\`, then create the
GitHub Release with notes sliced from that CHANGELOG.md. Implies publishing
(needs \`gh\` authenticated as the mirror repo's owner); --push is redundant
with it. Without a Release, HACS treats the repo as commit-based and shows
bare commit SHAs instead of the version.
EOF
  exit 2
}

mirror_path="$DEFAULT_MIRROR_PATH"
version=""
dry_run=0
push=0
release=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --mirror-path) mirror_path="${2:?--mirror-path needs a value}"; shift 2 ;;
    --version) version="${2:?--version needs a value}"; shift 2 ;;
    --dry-run) dry_run=1; shift ;;
    --push) push=1; shift ;;
    --release) release=1; shift ;;
    -h|--help) usage ;;
    *) echo "unknown argument: $1" >&2; usage ;;
  esac
done
if [[ "$dry_run" -eq 1 && ( "$push" -eq 1 || "$release" -eq 1 ) ]]; then
  echo "error: --dry-run cannot be combined with --push or --release." >&2
  exit 2
fi

repo_root="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
ha="${repo_root}/integrations/homeassistant"
src_cc="${ha}/custom_components/battery_soc"
mirror="${ha}/mirror"

# Default the release version to the monorepo manifest's "version" field --
# the pre-commit hook keeps it current (git-hooks/lib.sh COMPONENTS). It is a
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

# 1. Never publish a stale vendored core.
python3 "${repo_root}/scripts/vendor_core.py" --check

# 2. Integration source (drop caches).
mkdir -p "${mirror_path}/custom_components/battery_soc"
rsync -a --delete --exclude '__pycache__/' --exclude '*.pyc' \
  "${src_cc}/" "${mirror_path}/custom_components/battery_soc/"

# 3. Repo-root files + workflows + README screenshots.
cp "${mirror}/hacs.json" "${mirror}/README.md" "${mirror}/info.md" \
   "${mirror}/LICENSE" "${mirror}/AI-DISCLAIMER.md" "${mirror_path}/"
rm -rf "${mirror_path}/.github"
cp -r "${mirror}/.github" "${mirror_path}/.github"
rm -rf "${mirror_path}/docs"
mkdir -p "${mirror_path}/docs"
cp -r "${ha}/docs/img" "${mirror_path}/docs/img"
cp "${mirror}"/docs/*.md "${mirror_path}/docs/"

# 4. Rewrite the manifest's public fields (stdlib json, no jq).
# shellcheck source=/dev/null
source "${mirror}/release.env"
OWNER="${OWNER:?release.env must set OWNER}" \
REPO="${REPO:?release.env must set REPO}" \
VERSION="$version" \
python3 - "${mirror}/manifest.overrides.json" \
          "${mirror_path}/custom_components/battery_soc/manifest.json" <<'PY'
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
  echo "--- custom_components/battery_soc/manifest.json ---"
  cat "${mirror_path}/custom_components/battery_soc/manifest.json"
  exit 0
fi

# 5a. Sync only (no --release): commit the assembled tree, optionally push the
# branch. No tag, no CHANGELOG regen, no GitHub Release -- HACS keeps showing
# the last released version until the next --release run.
if [[ "$release" -eq 0 ]]; then
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

# 5b. Release (--release). First rebuild integrations/homeassistant/CHANGELOG.md
# from the monorepo history and stage it there -- it is a monorepo-tracked
# file, so this script only stages it; you commit it alongside the version
# bump.
"${repo_root}/scripts/generate_changelog.sh" ha-integration
git -C "$repo_root" add integrations/homeassistant/CHANGELOG.md
if git -C "$repo_root" diff --cached --quiet -- integrations/homeassistant/CHANGELOG.md; then
  echo "integrations/homeassistant/CHANGELOG.md unchanged."
else
  echo "note: integrations/homeassistant/CHANGELOG.md regenerated and staged in the monorepo -- commit it with the release."
fi

# 6. Commit + tag the mirror, then publish: push branch + tag and cut the
# GitHub Release. HACS only leaves commit mode (bare SHAs, dead "release
# announcement" link) once a Release exists.
if git -C "$mirror_path" rev-parse -q --verify "refs/tags/v${version}" >/dev/null; then
  echo "error: tag v${version} already exists in ${mirror_path}." >&2
  echo "       Bump the manifest version (commit it) or pass --version." >&2
  exit 1
fi
if [[ -n "$(git -C "$mirror_path" status --porcelain)" ]]; then
  git -C "$mirror_path" add -A
  git -C "$mirror_path" commit -q -m "release v${version}"
fi
git -C "$mirror_path" tag "v${version}"
echo "committed and tagged v${version} in ${mirror_path}"

branch="$(git -C "$mirror_path" rev-parse --abbrev-ref HEAD)"
git -C "$mirror_path" push origin "$branch"
git -C "$mirror_path" push origin "v${version}"

# Release notes: the "## vX.Y.Z ..." section of the changelog, up to the next
# "## " heading. Falls back to a one-liner if that version has no section yet.
notes_file="$(mktemp)"
trap 'rm -f "$notes_file"' EXIT
awk -v ver="v${version}" '
  $0 ~ "^## " ver "( |$|\\()" { grab = 1; next }
  grab && /^## / { exit }
  grab { print }
' "${ha}/CHANGELOG.md" > "$notes_file"
if [[ ! -s "$notes_file" ]]; then
  printf 'Release %s of the Battery SoC Home Assistant integration.\n' "v${version}" > "$notes_file"
fi

gh release create "v${version}" \
  --repo "${OWNER}/${REPO}" \
  --title "v${version}" \
  --notes-file "$notes_file"
echo "pushed ${branch} + v${version} and created the GitHub Release"
