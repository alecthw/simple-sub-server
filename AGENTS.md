# Repository Guidelines

## Project Structure & Module Organization

This is a Go service module (`github.com/alecthw/sub-server`) for serving subscription configuration files. Entry point code lives in `main.go`; shared initialization is in `handler/handler.go`.

Core request handling is split by feature:

- `handler/subscribe.go`: `/:uuid/:file` routing, security checks, fallback lookup, and response flow.
- `handler/subconv/`: `.ini` and subconverter compatibility, including redirect handling.
- `handler/template/`: template-based subscription injection for Clash, Stash, Egern, Surge, Loon, and QuanX.
- `handler/provider/`: provider subscription fetching.
- `handler/subscription/`: `subscribe.txt` parsing.
- `log/`: Zap logger setup.

The `sub/` directory is local runtime data and is ignored by Git. It may contain UUID directories, `subscribe.txt`, provider files, `sub/template`, and `sub/subconv` files for local verification.

## Build, Test, and Development Commands

- `go build -o sub-server`: build the local binary.
- `go run . -dir /home/alecthw/simple-sub-server -host 127.0.0.1:8080`: run the server against this workspace.
- `go run . -dir /path/to/workdir -host 127.0.0.1:8080 -subcnv http://127.0.0.1:25500 -mcp https://example.com/dlcfg`: run with subconverter and managed-config prefix.
- `go test ./...`: run all package tests.
- `gofmt -w <files>`: format touched Go files before committing.

## Coding Style & Naming Conventions

Use standard Go formatting and idioms. Keep package names short and lowercase. Prefer small feature-focused packages under `handler/` instead of expanding one large file. Keep request validation and file-access checks explicit, because this service exposes local files by URL.

## Testing Guidelines

Use Go's standard `testing` package. Place tests beside the package under test and name files `*_test.go`. Favor table-driven tests for routing decisions, whitelist behavior, fallback lookup, redirect generation, and template injection output. Run `go test ./...` before handing off changes.

## Commit & Pull Request Guidelines

Recent commits use short summaries, sometimes Chinese and sometimes numbered, such as `remove redirect` or `1. add url file support ...`. Keep commits concise and behavior-focused. For PRs, include the purpose, affected routes or template types, local test commands, and any required `sub/` runtime setup. Do not include private subscription URLs, UUID data, provider files, or generated binaries.

## Security & Configuration Tips

Treat `sub/` as private runtime configuration. Never expose `subscribe.txt`, `whitelist.txt`, provider files, or real subscription URLs in commits or logs. Preserve UUID validation, path traversal checks, extension allowlists, and whitelist enforcement when changing request handling.
