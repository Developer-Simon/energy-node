## Squash-merge commit message

<!--
  `main` uses squash merge, so ONE commit lands on `main` per PR. The fenced
  block below is that commit message — it is copied verbatim into GitHub's
  squash dialog at merge time (or flows through automatically when the repo
  defaults the squash message to the PR title and body).

  Keep it Conventional Commits, matching `git log`:
    type(scope): imperative summary under ~72 chars

  type  = feat | fix | docs | test | refactor | chore | ci | build | perf
  scope = area touched (dashboard, shelly, battery-soc, docs, …)

  Put the rationale in the body. Note breaking changes as a
  `BREAKING CHANGE:` footer and link issues with `Closes #123`.

  The PR title becomes the highlight of this release in the changelogs.
  To word it differently, add a paragraph to the body whose first line is
  the word Highlights followed by a colon, and below it one `- text` line
  (every component) or `- <target>: text` line (one component, e.g.
  `- dashboard: ...`, `- service:battery_soc: ...`). The paragraph ends at
  the first line that does not start with `- `. Details: docs/releasing.md.

  `scripts/dev/create-pr.sh` pre-fills this block from your branch commits and
  opens it in your editor; the first line also becomes the PR title.
-->

```text
type(scope): imperative summary under ~72 chars

Why this change is needed and what it does. Wrap the body at ~72 columns.

Closes #
```

## Summary

<!-- 1–3 sentences of context for the reviewer. Not the commit message. -->

## Checklist

- [ ] Subject line follows [Conventional Commits](https://www.conventionalcommits.org)
      (`type(scope): summary`) and matches the style in `git log`.
- [ ] Relevant checks from
      [CONTRIBUTING.md → Running the checks](../blob/main/CONTRIBUTING.md#running-the-checks)
      pass locally (Python bridges, HA integration, Go dashboard, dashboard JS,
      dashboard smoke test — whichever the change touches).
- [ ] `./scripts/dev/check_tracked_secrets.sh` is clean — no real
      credentials, private-range IPs, or device serials.
- [ ] AI assistance, if any, is disclosed per
      [AI-DISCLAIMER.md](../blob/main/AI-DISCLAIMER.md).
- [ ] For anything beyond a small fix: a linked issue describing the change.
