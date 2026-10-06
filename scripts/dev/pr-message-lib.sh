#!/usr/bin/env bash
#
# Shared helpers for scripts/dev/create-pr.sh and scripts/dev/merge-pr.sh.
#
# The squash-merge commit message of this repo has a fixed shape:
#
#   - the subject is the PR title, so the message itself does NOT repeat it;
#   - the body explains the change;
#   - the last line is a `Co-authored-by: Claude <Model> <x.y> <noreply@anthropic.com>`
#     trailer naming the model that did the work. Sonnet, Opus and Fable are
#     accepted, Haiku never lands on `main`.
#
# Source this file, it defines functions only.

PR_TRAILER_RE='^Co-authored-by: Claude (Sonnet|Opus|Fable) [0-9]+(\.[0-9]+)? <noreply@anthropic\.com>$'
PR_CC_RE='^[a-z]+(\([a-z0-9._:/-]+\))?!?: .+'

# pr_trailer <model> - "Sonnet 5.5" (or "Claude Sonnet 5.5") -> trailer line.
pr_trailer() {
  local model="${1#Claude }"
  printf 'Co-authored-by: Claude %s <noreply@anthropic.com>\n' "${model}"
}

# pr_normalize_message <title> [<model>]
# stdin: message. stdout: the message without a leading title line, without
# energy-node-bot / duplicate Claude trailers, ending in exactly one Claude
# trailer. <model> is used when the message carries no valid trailer itself.
pr_normalize_message() {
  local title="${1:-}" model="${2:-}" tmp trailer first
  tmp="$(mktemp)"
  cat > "${tmp}"

  # Drop a leading title line (and the blank line after it).
  first="$(awk 'NF { print; exit }' "${tmp}")"
  if [[ -n "${first}" && ( "${first}" == "${title}" || "${first}" =~ ${PR_CC_RE} ) ]]; then
    awk '!done && NF { done = 1; skip = 1; next } skip && !NF { skip = 0; next } { skip = 0; print }' \
      "${tmp}" > "${tmp}.2" && mv "${tmp}.2" "${tmp}"
  fi

  # A valid Claude trailer already present wins over <model>.
  trailer="$(grep -E "${PR_TRAILER_RE}" "${tmp}" | tail -n 1 || true)"
  if [[ -z "${trailer}" && -n "${model}" ]]; then
    trailer="$(pr_trailer "${model}")"
    trailer="${trailer%$'\n'}"
  fi

  # Remove every Claude / bot trailer and the "---------" squash separator,
  # then trim trailing blank lines.
  grep -v -i -E '^Co-authored-by: (Claude|energy-node-bot)|^-{5,}$' "${tmp}" \
    | awk '{ lines[NR] = $0 } END { n = NR; while (n > 0 && lines[n] ~ /^[[:space:]]*$/) n--; for (i = 1; i <= n; i++) print lines[i] }' \
    > "${tmp}.2" || true

  if [[ -s "${tmp}.2" ]]; then
    cat "${tmp}.2"
    printf '\n'
  fi
  [[ -n "${trailer}" ]] && printf '%s\n' "${trailer}"
  rm -f "${tmp}" "${tmp}.2"
}

# pr_validate_message <title> <message-file>
# Prints one problem per line to stderr, returns 1 if there is any.
pr_validate_message() {
  local title="$1" file="$2" rc=0 last first
  if [[ ! "${title}" =~ ${PR_CC_RE} ]]; then
    echo "title is not a Conventional Commits subject: ${title}" >&2
    rc=1
  fi
  first="$(awk 'NF { print; exit }' "${file}")"
  if [[ "${first}" == "${title}" ]]; then
    echo "message repeats the title as its first line" >&2
    rc=1
  fi
  if grep -q -i -E '^Co-authored-by:.*haiku' "${file}"; then
    echo "message carries a Haiku trailer, only Sonnet or Opus may be named" >&2
    rc=1
  fi
  last="$(awk 'NF { l = $0 } END { print l }' "${file}")"
  if [[ ! "${last}" =~ ${PR_TRAILER_RE} ]]; then
    echo "last line must be 'Co-authored-by: Claude Sonnet|Opus x.y <noreply@anthropic.com>', got: ${last}" >&2
    rc=1
  fi
  if [[ "$(grep -c -E "${PR_TRAILER_RE}" "${file}")" -gt 1 ]]; then
    echo "more than one Claude trailer" >&2
    rc=1
  fi
  return "${rc}"
}

# pr_extract_block <pr-body-file> - first fenced block, without the fences.
pr_extract_block() {
  awk '/^```/ { c++; next } c == 1 { print }' "$1"
}
