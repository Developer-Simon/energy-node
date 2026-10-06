## Squash-merge commit message

<!--
  `scripts/dev/create-pr.sh` fills every field of this template. Do not edit
  the placeholders by hand. Change the inputs and run the script again, or
  edit the PR description once and let `scripts/dev/merge-pr.sh` check it.

  `main` uses squash merge. The subject of the merge commit is the PR title
  (Conventional Commits: `type(scope): imperative summary`), so the fenced
  block below holds only the BODY. It never repeats the title and always ends
  with a `Co-authored-by: Claude Sonnet|Opus x.y <noreply@anthropic.com>`
  trailer. Haiku is never named there.

  `scripts/dev/merge-pr.sh` takes this block as the merge body. GitHub's own
  merge button would use the commit list instead, so merge with the script.

  Release highlights: add a paragraph to the body whose first line is
  `Highlights:` followed by `- text` lines (see docs/developing/releasing.md).
-->

```text
{{MESSAGE}}
```

## Summary

{{SUMMARY}}

## Checks

- [{{CHECK_TITLE}}] The title is a Conventional Commits subject.
- [{{CHECK_SECRETS}}] `scripts/dev/check_tracked_secrets.sh` is clean.
- [{{CHECK_AI}}] AI assistance disclosed per [AI-DISCLAIMER.md](../blob/main/AI-DISCLAIMER.md): {{AI_MODEL}}
- [{{CHECK_SUITES}}] Suites run locally: {{SUITES}}
- Linked issue: {{ISSUE}}
