# Contributing to Cyntr

We welcome contributions! Here's how to get started.

## Development Setup

```bash
git clone https://github.com/surya-koritala/cyntr.git
cd cyntr
go build -o cyntr ./cmd/cyntr
go test ./... -race
```

## Guidelines

- **Tests required** — every change must have tests. No mocks — use real SQLite, real IPC, real HTTP (httptest).
- **TDD preferred** — write the test first, verify it fails, then implement.
- **Single binary** — no external runtime dependencies. Pure Go. No CGO.
- **Minimal deps** — think twice before adding a new dependency.
- **Deny by default** — security features should fail closed, not open.

## Pull Requests

1. Fork the repo
2. Create a feature branch (`git checkout -b feat/my-feature`)
3. Write tests + implementation
4. Run `go test ./... -race && go vet ./...`
5. Commit with descriptive messages
6. Open a PR against `main`

## Architecture

Cyntr uses a microkernel architecture. Every component is a `kernel.Module` that communicates via the IPC bus. When adding new functionality:

1. Create a new module implementing `kernel.Module`
2. Register IPC handlers in `Start()`
3. Add tests in the same package
4. Register the module in `cmd/cyntr/main.go`

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Keep files focused — one responsibility per file
- Prefer explicit over clever
- Error messages should be actionable

## Reporting Issues

Open an issue at https://github.com/surya-koritala/cyntr/issues
