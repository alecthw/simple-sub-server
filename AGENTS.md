# Repository Guidelines

## Project Structure & Module Organization

This is a Go service module (`github.com/alecthw/sub-server`) for serving subscription configuration files. Entry point code lives in `main.go`; per-instance server construction and route registration are in `handler/handler.go`.

Core request handling is split by feature:

- `handler/subscribe.go`: `/:uuid/:file` HTTP orchestration and response flow.
- `internal/filestore/`: UUID/path validation, whitelist enforcement, root-scoped reads, and fallback lookup.
- `internal/subconv/`: `.ini` and subconverter compatibility, including redirect handling.
- `internal/template/`: template-based subscription injection for Clash, Stash, Egern, Surge, Loon, and QuanX.
- `internal/provider/`: provider subscription fetching.
- `internal/subscription/`: `subscribe.txt` parsing.
- `log/`: Zap logger setup.

The `sub/` directory is local runtime data and is ignored by Git. It may contain UUID directories, `subscribe.txt`, provider files, `sub/template`, and `sub/subconv` files for local verification.

## Build, Test, and Development Commands

- `go build -o sub-server`: build the local binary.
- `go run . -dir /path/to/workdir -host 127.0.0.1:8080`: run the server against this workspace.
- `go run . -dir /path/to/workdir -host 127.0.0.1:8080 -subcnv http://127.0.0.1:25500 -mcp https://example.com/dlcfg`: run with subconverter and managed-config prefix.
- `go test ./...`: run all package and HTTP end-to-end tests.
- `go test -race ./...`: verify concurrent request handling.
- `go vet ./...`: run static checks.
- `gofmt -w <files>`: format touched Go files before committing.

## Coding Style & Naming Conventions

Use standard Go formatting and idioms. Keep package names short and lowercase. Keep HTTP orchestration under `handler/` and feature-focused implementation packages under `internal/`. Template injectors compose typed steps through `Codec[T]` and `pipeline[T]`; use the per-server registry for app selection. Keep request validation and file-access checks explicit, because this service exposes local files by URL.

## Testing Guidelines

Use Go's standard `testing` package. Place tests beside the package under test and name files `*_test.go`. Favor table-driven tests for routing decisions, whitelist behavior, fallback lookup, redirect generation, and template injection output. Run `go test ./...` before handing off changes.

## Commit & Pull Request Guidelines

Recent commits use short summaries, sometimes Chinese and sometimes numbered, such as `remove redirect` or `1. add url file support ...`. Keep commits concise and behavior-focused. For PRs, include the purpose, affected routes or template types, local test commands, and any required `sub/` runtime setup. Do not include private subscription URLs, UUID data, provider files, or generated binaries.

## Security & Configuration Tips

Treat `sub/` as private runtime configuration. Never expose `subscribe.txt`, `whitelist.txt`, provider files, or real subscription URLs in commits or logs. Preserve UUID validation, path traversal checks, extension allowlists, and whitelist enforcement when changing request handling.
