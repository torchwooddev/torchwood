# Torchwood

**English** | [简体中文](README_ZH.md)

Torchwood is an Appwrite-inspired, **AI/Agent-Native** Backend-as-a-Service (BaaS) platform built with Go, PostgreSQL, and gRPC/grpc-gateway. It provides user authentication, a dynamic document database, file storage, function execution, and an Admin Console — with APIs and tooling designed for LLM agents, automation, and MCP tool servers from day one.

## Features

- **AI / Agent-Native**: Protobuf-first APIs with auto-generated OpenAPI/Swagger specs; scoped API Keys for autonomous Server-side automation; predictable JSON REST surface and structured errors; TypeScript SDK for agent workflows and tool integration.
- **Project management**: Multi-project isolation; each project gets its own database schema.
- **User authentication**: Email sign-up/sign-in, JWT access/refresh tokens, session cookies, and API Key auth.
- **Dynamic document database**: Schema-per-database with `_tenant`, `_perms`, dynamic attributes/indexes, and an Appwrite-style query DSL.
- **File storage**: S3/MinIO-compatible object storage with multipart upload/download; file metadata managed as dynamic documents.
- **Function execution**: Docker-based executor (build/run) with an async worker (`cmd/worker`).
- **Admin Console**: React + Vite + TanStack Query + shadcn/ui admin UI, embedded in the Go binary at `/console/`.
- **Server API**: CRUD for Projects, API Keys, Users, Storage, Databases, Collections, Attributes, and Indexes.

## Tech Stack

### Backend

- Go 1.25
- [Lynx](https://github.com/lynx-go/lynx) service framework
- gRPC + grpc-gateway
- [Wire](https://github.com/google/wire) dependency injection
- [bun](https://github.com/uptrace/bun) ORM (metadata tables)
- PostgreSQL (dynamic document layer)
- Redis
- MinIO / S3 (object storage)

### Frontend

- React 19 + TypeScript 6
- Vite 8
- React Router 7
- TanStack Query 5
- Tailwind CSS 3 + shadcn/ui-style components
- sonner (toast)
- lucide-react

## Quick Start

### Prerequisites

- Go 1.25+
- Node.js 22+ and pnpm
- Docker + Docker Compose
- [Task](https://taskfile.dev/) (`go install github.com/go-task/task/v3/cmd/task@latest`)

### 1. Start local infrastructure

```bash
task up
```

This starts PostgreSQL (5432), Redis (6379), and MinIO (9000/9001). Ports are configurable via the `POSTGRES_PORT`, `REDIS_PORT`, `MINIO_API_PORT`, and `MINIO_CONSOLE_PORT` variables in `.env`.

### 2. Configure environment variables

Copy the template and fill in required values:

```bash
cp .env.example .env
```

Key variables:

```env
TORCHWOOD_DATA_DATABASE_SOURCE=postgres://torchwood:torchwood@127.0.0.1:5432/torchwood?sslmode=disable
TORCHWOOD_DATA_REDIS_PASSWORD=            # Redis addr comes from data.redis.addr in configs/config.yaml
TORCHWOOD_SECURITY_JWT_SECRET=change-me-in-production
TORCHWOOD_STORAGE_S3_ENDPOINT=http://127.0.0.1:9000
TORCHWOOD_STORAGE_S3_ACCESS_KEY_ID=minioadmin
TORCHWOOD_STORAGE_S3_SECRET_ACCESS_KEY=minioadmin
```

### 3. Run database migrations

```bash
task migrate
```

### 4. Install dependencies and seed data

```bash
# Install tools (first time)
task install-tools

# Install Console dependencies
task console-install

# Generate protobuf, wire, etc.
task generate-all

# Create default project and Console admin
go run ./cmd/seed
```

Default admin: `admin@torchwood.local / Admin@123`.

### 5. Build and run

```bash
task build      # runs console-build, then compiles the Go server
./bin/server.exe
```

Or use dev mode:

```bash
task dev-server
```

Endpoints (defaults from `configs/config.yaml.template`; not hardcoded):

- Admin Console: `http://127.0.0.1:9080/console/`
- HTTP/gRPC-gateway API: `http://127.0.0.1:9080/v1/...`
- gRPC (loopback only): `127.0.0.1:9060`
- Metrics: `http://127.0.0.1:9040/metrics`
- Health: `http://127.0.0.1:9080/healthz/liveness`, `/healthz/readiness`

> Note: `console/vite.config.ts` and the CORS `allow_origins` in the config template still reference the legacy port 9099; adjust them (or your local `configs/config.yaml`) if you use `task console-dev`.

## Common Development Tasks

```bash
# Infrastructure
task up          # docker compose up
task down        # docker compose down
task migrate     # run database migrations

# Code generation
task generate-proto    # buf generate
task generate-config   # generate Go config
task wire-all          # regenerate Wire
task generate-all      # all of the above

# Frontend
task console-install   # npm install
task console-build     # npm run build
task console-dev       # npm run dev

# Backend
task dev-server        # go run ./cmd/server
task test              # go test -v ./... -cover
task build             # build full binary (includes console)
```

## Project Structure

```
.
├── cmd/
│   ├── seed/              # default project / admin / API key bootstrap
│   ├── server/            # server entrypoint and Wire assembly
│   └── worker/            # async worker (functions executions queue consumer)
├── console/               # Admin Console React SPA
│   ├── embed.go           # go:embed dist
│   └── src/
├── configs/               # config templates
├── db/migrations/         # golang-migrate SQL migrations
├── docker/local/          # local Docker Compose
├── docs/                  # design documents
├── genproto/              # generated protobuf code
├── internal/
│   ├── api/               # gRPC handlers / custom HTTP handlers
│   │   ├── clientgrpc/
│   │   ├── consolegrpc/
│   │   ├── servergrpc/
│   │   └── serverhttp/
│   ├── app/               # use cases
│   │   ├── client/        # Account sign-up/sign-in
│   │   ├── console/       # Console auth
│   │   ├── functions/     # Functions use case
│   │   ├── server/        # Projects / API keys / users / databases
│   │   └── storage/       # File / bucket metadata
│   ├── domain/            # domain models and ports
│   │   ├── databases/
│   │   ├── functions/
│   │   ├── projects/
│   │   ├── shared/
│   │   └── storage/
│   ├── infra/             # adapter implementations
│   │   ├── auth/          # Principal / Validator
│   │   ├── bun/           # metadata repositories
│   │   ├── clients/       # PG / Redis / S3 clients
│   │   ├── documentdb/    # PostgreSQL dynamic document adapter
│   │   ├── functions/     # Docker executor stub
│   │   ├── server/        # gRPC / gateway / metrics / console server
│   │   └── storage/       # MinIO object storage
│   ├── pkg/config/        # protobuf config schema
│   └── testutil/          # integration test helpers
├── pkg/
│   ├── crud/              # list / pagination / sort utilities
│   ├── grpc/interceptor/  # auth interceptors
│   ├── idgen/             # ID generation
│   ├── jwtparser/         # JWT issue / parse
│   ├── password/          # password hashing
│   └── query/             # Appwrite-style query DSL
├── proto/                 # protobuf source files
├── sdk/                   # TypeScript SDK and demo app
├── buf.yaml / buf.gen.yaml
├── go.mod
├── Taskfile.yml
└── README.md
```

## Architecture

- **Clean Architecture / DDD**: domain defines ports, infra provides implementations, app orchestrates use cases, api handles transport.
- **AI / Agent-Native API design**: protobuf is the single source of truth; `buf generate` produces gRPC stubs, grpc-gateway handlers, and OpenAPI specs under `genproto/`. The **Server API** (`/v1/server/*`) is scoped for programmatic and agent access via API Keys; the **Client API** (`/v1/account/*`, `/v1/databases/*`, etc.) serves end-user flows. See [`sdk/README.md`](sdk/README.md) for the TypeScript SDK.
- **Dynamic document database**: each database maps to a PostgreSQL schema; collections are real tables; `_tenant` isolates projects; `_perms` implements role-based document permissions.
- **Authentication**: end-user JWT, session cookies, API Keys, and console admin JWT. API Keys do not bypass `_perms`—they participate as the `keys` role; admins can target a project via the `X-Torchwood-Project` header.
- **REST API**: gRPC methods are exposed as JSON REST via grpc-gateway; file upload/download uses custom HTTP handlers.
- **Console**: the React SPA is embedded into the Go binary via `//go:embed dist` and served at `/console/`.

## Testing

```bash
# Unit / integration tests (requires local Postgres)
task test
```

Integration tests include:

- `internal/infra/documentdb/postgres_test.go`
- `internal/app/client/account_test.go`

Tests automatically create and drop the `TORCHWOOD_test` database.

The test database DSNs are read from the `TORCHWOOD_TEST_DATABASE_SOURCE` and `TORCHWOOD_TEST_ADMIN_DATABASE_SOURCE` environment variables (defined in `.env.example`); `task test` loads them from `.env` automatically. When running `go test` directly, export them first:

```bash
# go test ./...               # fails fast without the variables
task test                     # loads .env and runs everything
```

## TypeScript SDK

See [`sdk/README.md`](sdk/README.md) for the `@torchwood/sdk` package and web demo.

```bash
task sdk-install
task sdk-build
task sdk-demo   # demo at http://localhost:5174
```

## Developer Documentation

Full developer docs (architecture, configuration, authentication, databases, storage, functions, API guide, Console, testing, SDK, operations) live in [`docs/developer/`](docs/developer/README.md).

## Design Documents

- `docs/roadmap.md` — development roadmap (includes AI/Agent-Native strategy)
- `docs/tech-decision.md` — technology decisions
- `docs/developer/` — developer documentation (see index above)
- `docs/archived/` — archived design docs (P0 design, migration checklist, security reviews, fix plans)

## License

MIT (TBD)
