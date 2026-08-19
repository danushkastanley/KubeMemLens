# Repository Agent Instructions

## Git Branch Naming

- Never create branches with `codex/`, `agent/` or other tool-specific prefixes.
- Use the prefix that best matches the work: `feature/`, `hotfix/`, `patch/`, `fix/` or `bug/`.
- Use a short, lowercase, kebab-case description after the prefix, for example `feature/tui-dashboard` or `fix/history-refresh-race`.
- Do not rename or replace a user-created branch unless the user explicitly requests it.

## Commit Metadata

- Apply these metadata rules to every commit created in this repository.
- Do not add Codex, OpenAI, agent or tool attribution to commit messages.
- Do not add automated `Co-authored-by` trailers or similar metadata unless the user explicitly requests them.
- Keep commit authorship and trailers limited to the human-configured Git identity and metadata the user has explicitly approved.
