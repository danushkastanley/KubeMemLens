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

## Pull Request Reviewer

- Request `legolas296` as a reviewer on every pull request you open in this repository, for example `gh pr create --reviewer legolas296`.
- For an existing pull request, check its review requests first and add the reviewer only if missing: `gh pr edit <number> --add-reviewer legolas296`.

## Release tag gate

- Before creating or pushing any release tag, read `README.md` and `docs/release-process.md` from the exact commit that will be tagged.
- Confirm the target version and maturity label agree across `README.md`, `CHANGELOG.md`, `SECURITY.md`, `charts/kube-memlens/Chart.yaml`, `docs/installation.md`, `docs/compatibility.md`, and other release-facing documents.
- Search the repository for stale version numbers, obsolete tag names, unsupported compatibility claims, missing files, and broken local Markdown links.
- Do not create or push the tag until the documentation changes are reviewed and merged to `main`, the target tag and release do not already exist, and the relevant checks pass.
- Push only the annotated tag. Let the release workflow create the GitHub draft release, then inspect the workflow, assets, image, chart, signatures, attestations, and `release-subjects.txt` before publication.
