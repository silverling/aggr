# AGENTS.md

- Every Go function and struct declaration must have a detailed comment immediately above it, including unexported helpers and test code.
- Regenerate `server/internal/webui/dist/index.html` with `pnpm --dir web build` before any Go build; the file is generated and should not be tracked in git.
- Prefer `make test` and `make build` for the normal verification workflow.
