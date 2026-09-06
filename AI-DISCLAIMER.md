# AI disclaimer

## How this project was built

Large parts of this repository — source code, tests, documentation and
tooling — were written with the help of an AI coding assistant:

- **Assistant:** Claude (Anthropic), used through **Claude Code**.
- **Human author and maintainer:** [@Developer-Simon](https://github.com/Developer-Simon),
  who reviews every change and is responsible for what ships and what runs on
  the actual hardware.

AI assistance does not lower the bar: every line is read, and changes are
checked against the relevant test suite (`go test`, `pytest`, `npm test`, the
local dashboard smoke test) before they are considered done. If you find a
bug, it is the maintainer's bug, not the model's.

This mirrors the same disclosure already made for the separately published
[`ha-battery-soc`](https://github.com/Developer-Simon/ha-battery-soc) HACS
integration — see its
[`AI-DISCLAIMER.md`](integrations/homeassistant/mirror/AI-DISCLAIMER.md).

## Contributing: disclose your AI use

If you open a pull request, **state which AI tool(s), if any, assisted with
the code** — in the PR description. This is a hard requirement, not a
preference; see [CONTRIBUTING.md](CONTRIBUTING.md).

## Why

This project runs unattended automation against real electrical loads and a
battery bank at a remote site. Knowing how a change was produced is part of
being able to trust it — the same reason the code is open in the first
place.
