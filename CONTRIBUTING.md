# Contributing to Cyntr

We welcome contributions! Cyntr is an open-source enterprise AI agent platform built in Go.

## Development Setup

```bash
# Clone and build
git clone https://github.com/surya-koritala/cyntr.git
cd cyntr
go build -o cyntr ./cmd/cyntr

# Run all tests (505 tests, 32 packages)
go test ./... -race

# Verify
go vet ./...

# Check your setup
./cyntr doctor
```

**Requirements:** Go 1.22+

## Project Structure

```
cyntr/
├── cmd/cyntr/        # CLI entrypoint (init, start, doctor, subcommands)
├── kernel/           # Microkernel core
│   ├── ipc/          # In-process message bus (request/reply + pub/sub)
│   ├── config/       # YAML config store + schema migration
│   ├── resource/     # Per-tenant resource tracking
│   └── log/          # Structured JSON logger
├── modules/          # Kernel modules (each implements kernel.Module)
│   ├── agent/        # AI runtime (providers, tools, sessions, memory, workflows)
│   ├── audit/        # Tamper-evident logging
│   ├── channel/      # Messaging adapters (Slack, Teams, etc.)
│   ├── federation/   # Multi-site distribution
│   ├── notify/       # Notification system
│   ├── policy/       # Security policy engine + approvals
│   ├── proxy/        # HTTP reverse proxy
│   ├── scheduler/    # Cron jobs
│   ├── skill/        # Skill management
│   └── workflow/     # Workflow engine
├── auth/             # RBAC, OIDC, JWT, API keys
├── tenant/           # Multi-tenancy, process isolation, Docker
├── web/              # REST API + dashboard
│   └── api/          # 22 endpoints with auth middleware
└── tests/integration/
```

## Guidelines

### Code
- **Single binary** — no external runtime dependencies. Pure Go. No CGO.
- **Minimal deps** — think twice before adding a dependency. Currently only `gopkg.in/yaml.v3` and `modernc.org/sqlite`.
- **Deny by default** — security features fail closed, not open.
- **Keep files focused** — one responsibility per file. If a file grows beyond ~300 lines, consider splitting.
- **Follow Go conventions** — `gofmt`, `go vet`, standard naming.

### Testing
- **No mocks** — use real SQLite, real IPC bus, real HTTP (httptest). If a test can't fail when the code is broken, it's not a test.
- **TDD preferred** — write the test first, verify it fails, then implement.
- **Race detection** — all tests must pass with `go test -race`.
- **Integration tests** — end-to-end tests live in `tests/integration/`.

### Architecture
Every component is a `kernel.Module` that communicates via the IPC bus. To add new functionality:

1. **Create the module package** under `modules/`
2. **Implement `kernel.Module`** interface:
   ```go
   type Module interface {
       Name() string
       Dependencies() []string
       Init(ctx context.Context, services *Services) error
       Start(ctx context.Context) error
       Stop(ctx context.Context) error
       Health(ctx context.Context) HealthStatus
   }
   ```
3. **Register IPC handlers** in `Start()` for inter-module communication
4. **Add tests** in the same package (no mocks)
5. **Register the module** in `cmd/cyntr/main.go`
6. **Add API endpoints** in `web/api/` if needed

### Adding a new tool
1. Create `modules/agent/tools/mytool.go` implementing the `Tool` interface
2. Add tests in `modules/agent/tools/mytool_test.go`
3. Register in `cmd/cyntr/main.go`: `toolReg.Register(agenttools.NewMyTool())`

### Adding a new channel adapter
1. Create `modules/channel/mychannel/adapter.go` implementing `ChannelAdapter`
2. Add tests with `httptest` for webhook endpoints
3. Register in `cmd/cyntr/main.go` gated by an environment variable

### Adding a new model provider
1. Create `modules/agent/providers/myprovider.go` implementing `ModelProvider`
2. Add tests with `httptest` mocking the LLM API
3. Register in `cmd/cyntr/main.go` gated by an environment variable

## Pull Requests

1. Fork the repo
2. Create a feature branch: `git checkout -b feat/my-feature`
3. Write tests + implementation
4. Run: `go test ./... -race && go vet ./...`
5. Commit with descriptive messages (use conventional commits: `feat:`, `fix:`, `docs:`, `test:`)
6. Open a PR against `main`

### PR checklist
- [ ] Tests pass (`go test ./... -race`)
- [ ] No lint issues (`go vet ./...`)
- [ ] Binary builds (`go build -o cyntr ./cmd/cyntr`)
- [ ] New features have tests
- [ ] README updated if adding user-facing features

## Reporting Issues

Open an issue at https://github.com/surya-koritala/cyntr/issues

Include:
- Cyntr version (`cyntr version`)
- Go version (`go version`)
- OS/architecture
- Steps to reproduce
- Expected vs actual behavior
