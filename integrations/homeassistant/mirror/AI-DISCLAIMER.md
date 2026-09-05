# AI disclaimer

## How this project was built

Large parts of this integration &mdash; source code, tests, documentation and
release tooling &mdash; were written with the help of an AI coding assistant:

- **Assistant:** Claude (Anthropic), used through **Claude Code**.
- **Human author and maintainer:** [@Developer-Simon](https://github.com/Developer-Simon),
  who reviews every change and is responsible for what ships.

AI assistance does not lower the bar: every line is read, tested against the
`battery_soc_core` suite, and validated by hassfest and HACS before a release is
tagged. If you find a bug, it is the maintainer's bug, not the model's.

## Contributing: disclose your AI use

If you open a pull request, **you must state which AI tool(s), if any, assisted
with the code** &mdash; in the PR description. This is a hard requirement, not a
preference.

- Name the assistant and interface, e.g. "Claude Code", "GitHub Copilot",
  "ChatGPT", "Cursor", "local Llama 3", or "none &mdash; hand-written".
- Roughly say what it did: full drafting, autocomplete only, test generation,
  refactor, docs.
- Add a commit trailer when a model did substantive work, so it is visible in
  `git log`:

  ```
  Assisted-by: Claude (Anthropic) via Claude Code
  ```

  or the model's own `Co-Authored-By:` line if it emits one.

PRs without an AI-use statement will be asked for one before review.

## Why

This integration estimates the state of charge of a battery bank for monitoring
and diagnostics. Knowing how a change was produced is part of being able to
trust it &mdash; the same reason the code is open in the first place.
