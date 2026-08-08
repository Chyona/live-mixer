# Live Clip Workbench

[中文](README.md) · [English](README.en.md)

> **AI-assisted live stream clipping** · Speech-to-text · Smart clip selection · CapCut drafts · One-click export  
> Technical repo name: `live-mixer` · Help creators and ops teams cut highlights much faster

Stack: Go 1.25+ · PostgreSQL 14+ · Docker Compose · see LICENSE in this repo

---

## Why Live Clip Workbench?

An end-to-end pipeline for **live replay / recorded streams**: ingest media, run ASR, let AI pick highlight segments, then generate CapCut drafts and optional final videos. Use fully automatic **one-click export**, or **AI draft + human refine** when quality matters most.

**Who it is for**

- Live commerce, education, gaming, and entertainment teams that ship clips every day
- Creators and editors who want LLMs to speed up “listen and select”, then finish polish in CapCut
- Engineering teams that need a self-hosted backend for live clipping / remix workflows

---

## Core features

| Feature | Description |
|---------|-------------|
| Live material management | Register replay / recording URLs (file or m3u8); auto-detect resolution and duration |
| Speech recognition (ASR) | Async transcription with sentences, summaries, and paragraph structure; subtitle download |
| Editing projects | Bind materials and system prompts; maintain rough ranges `clips0` and refined picks `clips1` |
| AI live clipping | Filter high-value ASR spans by prompt and rough ranges; write back `clips1` |
| CapCut draft generation | Precise cuts via ffmpeg; call capcut-mate for drafts you can keep editing |
| One-click export | Chains “AI clip → draft → (optional) video render” for batch throughput |
| AI + human collaboration | Refine `clips1` after AI, then run a draft-only job |

### Typical workflow

```text
Ingest live material
  → ASR / summary
  → Create editing project (material + prompt, optional clips0)
  → Path A: one-click export (AI clip + draft + optional video)
  → Path B: AI clip → manually refine clips1 → generate draft
  → Download CapCut draft for finishing
```

---

## Tech stack

| Layer | Stack |
|-------|--------|
| API / workers | Go, Gin, async job scheduling |
| Database | PostgreSQL 14+ |
| Speech-to-text | Doubao BigModel ASR |
| LLM | OpenAI-compatible APIs (e.g. Alibaba DashScope) |
| CapCut drafts | capcut-mate |
| Object storage | Tencent COS / Alibaba OSS / Volcengine TOS (pick one) |
| Deploy | Docker Compose (API + capcut-mate + nginx) |

---

## Quick start (Docker)

### Prerequisites

- Docker / Docker Compose
- Reachable **PostgreSQL 14+** (create the database first; the app does not bundle a DB)
- Configured object storage, ASR key, and LLM key

### 1. Environment variables

```bash
cd docker
cp .env.example .env
```

Fill at least: `APP_DATABASE_*`, `APP_JWT_SECRET`, `APP_STORAGE_*`, `APP_ASR_API_KEY`, `APP_LLM_API_KEY`, `APP_CAPCUT_MATE_BASE_URL`.

Inside Compose, CapCut base URL is usually `http://nginx`. Optional:

- `APP_CAPCUT_MATE_GEN_VIDEO_BASE_URL` — dedicated root for gen_video APIs (falls back to `BASE_URL`)
- `APP_CAPCUT_MATE_ENABLE_GEN_VIDEO` — enable final video render (default on; off skips render and completes after draft)

Config priority: **`APP_*` env vars > external yaml > embedded defaults**.

### 2. Initialize the database

```bash
go run ./cmd/envinit init
# or
docker exec -it live-mixer /app/envinit init
```

Default account: `admin` / `admin` (change immediately in production).

```bash
go run ./cmd/envinit reset-password -p 'YourStrongPassword'
```

### 3. Start and verify

```bash
cd docker
docker compose up -d
# local build: docker compose up -d --build

curl http://localhost:30000/health
curl http://localhost:81/openapi/live-mixer/v1/auth/login
```

| Service | Role | Host port |
|---------|------|-----------|
| `live-mixer` | API + background workers | `30000` |
| `capcut-mate` | CapCut draft service | `31001` |
| `nginx` | Static assets + reverse proxy | `81` |

Logs: `docker/logs/` · Staging: `docker/html/`

> When deploying to a new network, update `PROXY_*` / `DRAFT_URL` in `docker-compose.yaml` to match your gateway and domain.

---

## Main APIs

JWT required except login. Full docs: Swagger at `http://localhost:30000/swagger/index.html`.

| Method | Path | Description |
|--------|------|-------------|
| POST | `/openapi/live-mixer/v1/auth/login` | Login and obtain token |
| CRUD | `/openapi/live-mixer/v1/live-materials` | Live materials |
| CRUD | `/openapi/live-mixer/v1/video-projects` | Editing projects |
| CRUD | `/openapi/live-mixer/v1/llm-system-prompts` | System prompts |
| POST | `/openapi/live-mixer/v1/tasks/ai-slice` | AI clipping job |
| POST | `/openapi/live-mixer/v1/tasks/draft` | CapCut draft job |
| POST | `/openapi/live-mixer/v1/tasks/ai-slice-draft` | One-click export job |
| GET | `/openapi/live-mixer/v1/tasks/:id` | Poll progress / draft & video URLs |

---

## Local development

### Requirements

- Go **1.25+**
- PostgreSQL **14+**
- ffmpeg (ASR audio extract and clip cutting; included in the Docker image)

### Layout

```text
live-mixer/
├── cmd/webserver/     # HTTP API + workers
├── cmd/envinit/       # schema / seed / reset password
├── internal/          # handler · service · repository · draft · scheduler …
│   └── config/        # local config.yaml (gitignored); example committed
├── docker/            # Dockerfile, compose, .env.example
├── docs/              # generated Swagger
├── migrations/        # reference SQL
└── pkg/               # small shared utilities
```

### Common commands

```bash
go mod download
cp internal/config/config.yaml.example internal/config/config.yaml

go run ./cmd/envinit init
go run ./cmd/webserver

go build -o webserver.exe ./cmd/webserver
go build -o envinit.exe ./cmd/envinit

go test ./...
```

Default listen address: `http://localhost:30000`. Override secrets via env, for example:

```bash
# Windows PowerShell
$env:APP_DATABASE_PASSWORD="your_password"
$env:APP_LLM_API_KEY="sk-xxx"
$env:APP_CAPCUT_MATE_API_KEY="your-capcut-api-key"
```

`envinit` subcommands: `schema` · `seed` · `init` · `reinit` (wipe & rebuild) · `reset-password`.

Without a full stack locally, run `docker compose up -d capcut-mate nginx` and point `capcut_mate.base_url` to `http://localhost:81`.

### Contributing tips

- Layers: `handler` → `service` → `repository` → `model`; async jobs in `scheduler`, CapCut pipeline in `draft`
- New APIs: add Swagger comments, register in `internal/routes/v1`, put auth-required routes under `JWTAuth`
- New config: update `config.yaml.example`, structs, and `docker/.env.example`
- AI clip / draft / one-click jobs are async; clients poll `GET /tasks/:id`

### Publishing images

Pushing a tag like `v1.0.0` triggers GitHub Actions to build and push the Docker Hub image.

---

## Docs & languages

| Document | Description |
|----------|-------------|
| [README.md](README.md) | Chinese docs (default) |
| [README.en.md](README.en.md) | English documentation |
| [LICENSE](LICENSE) | License |

Keywords: live stream clipping, AI clipping, live remix, speech recognition ASR, CapCut draft, one-click export, Live Clip Workbench, live-mixer

---

## License

See [LICENSE](./LICENSE) in the repository root.
