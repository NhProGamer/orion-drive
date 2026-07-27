<div align="center">

# OrionDrive

**Self-hosted file management, in a single binary.**

A Go backend serving an embedded Vue 3 SPA — files, sharing, WebDAV/SFTP, previews,
Office editing and multi-backend storage, with SSO-only authentication.

[![CI](https://git.nhsoul.fr/nhpro/orion-drive/actions/workflows/ci.yaml/badge.svg)](https://git.nhsoul.fr/nhpro/orion-drive/actions)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8.svg)](https://go.dev)
[![Vue](https://img.shields.io/badge/Vue-3-42b883.svg)](https://vuejs.org)

</div>

---

OrionDrive is a clean-room, self-hosted drive: one static binary that embeds the web app,
talks to SQLite / PostgreSQL / MySQL, and stores objects on local disk, S3-compatible
storage or a remote node. Authentication is **OIDC / SSO only** — there are no local
password accounts.

> Not affiliated with, and containing no code from, any GPL-licensed project. Licensed under **MIT**.

## Features

- **Files** — explorer with folders, resumable chunked upload, rename/move, trash & restore,
  versioning, file locking, and per-group quotas.
- **Search** — recursive, filterable (type, category, starred, modified date), with each hit's location.
- **Storage backends** — local, S3-compatible and remote (slave-node) drivers, with direct/presigned
  downloads and per-group speed limits. Optional AES-256-CTR encryption at rest.
- **Sharing** — file & folder links with permission levels (view / edit / blind deposit), passwords,
  expiry and download limits; rich OpenGraph unfurls on paste.
- **Access protocols** — WebDAV over `/dav` and SFTP (resumable), both with dedicated per-user
  credentials independent of SSO; personal access tokens for API clients.
- **Preview & editing** — image, video, audio, PDF, text, Markdown and ePub previews; an image
  editor; and collaborative Office editing via WOPI (OnlyOffice / Collabora) with document locking.
- **Archives** — background compress/extract (zip / tar / 7z) and grouped ZIP downloads.
- **Administration** — admin panel; groups with granular permissions and per-group storage policies;
  SSO-group → group/admin mapping; scheduled maintenance (trash purge, upload cleanup).
- **Platform** — SQLite / PostgreSQL / MySQL, optional Redis-backed shared cache for multi-node
  deployments, an installable PWA, dark/light themes and full i18n (English & French).

## Quick start (Docker)

```bash
docker run -p 5212:5212 -v orion-data:/app/data \
  -e OD_CONF_System_SessionSecret="$(openssl rand -hex 32)" \
  -e OD_CONF_System_SiteURL="https://drive.example.com" \
  -e OD_CONF_OIDC_Issuer="https://id.example.com" \
  -e OD_CONF_OIDC_ClientID="orion" \
  -e OD_CONF_OIDC_ClientSecret="…" \
  git.nhsoul.fr/nhpro/orion-drive:latest
```

Or with Compose (bundles PostgreSQL, Redis and OnlyOffice behind opt-in profiles):

```bash
docker compose up -d          # http://localhost:5212
```

## Configuration

Every setting is an INI key (`conf.ini`) overridable by an `OD_CONF_<Section>_<Key>` environment
variable — see [`conf.ini.example`](conf.ini.example) for the full, documented set. At minimum set
`System.SessionSecret`, `System.SiteURL` and the `OIDC.*` values. The published image runs in
`release` mode, so the debug dev-login is compiled out.

## Development

```bash
cp conf.ini.example conf.ini      # configure OIDC (or use dev-login in debug mode)

# Backend — API + embedded SPA on :5212 (runs pending migrations on startup)
go run . server

# Frontend — Vite dev server with API proxy + hot reload
cd frontend && npm install && npm run dev
```

Useful CLI: `go run . migrate up`, `go run . group …`, `go run . policy add-s3|add-local`.

## Production build

```bash
cd frontend && npm run build      # emits application/statics/dist (embedded by the binary)
cd .. && go build -o orion-drive .
./orion-drive server
```

The multi-stage `Dockerfile` does both steps and ships a minimal Alpine runtime (with `ffmpeg` for
thumbnails); the binary is static (`CGO_ENABLED=0`).

## Tech stack

| | |
|---|---|
| **Backend** | Go 1.26 · Gin · GORM (SQLite / PostgreSQL / MySQL, pure-Go) · goose migrations · cobra · coreos/go-oidc · `x/net/webdav` · `pkg/sftp` · aws-sdk-go-v2 · optional Redis |
| **Frontend** | Vue 3 · Vite · TypeScript · Pinia · vue-router · vue-i18n · vite-plugin-pwa · lucide · a custom **"Nebula"** CSS design system (oklch tokens, dark/light) |
| **Release** | GoReleaser · Forgejo Actions · Docker |

## Project layout

```
cmd/            CLI commands (server, migrate, version, group, policy)
conf/           configuration (INI + env overrides)
application/    bootstrap (DI container) and embedded SPA
migrations/     goose SQL migrations, per dialect (embedded)
model/          GORM models
repository/     data-access layer
service/        business logic
pkg/            filemanager (drivers/encrypt), auth, cache, queue, crontab,
                archive, thumb, wopi, webdav, sftpserver, serializer
middleware/     Gin middleware
routers/        HTTP routes and controllers
frontend/       Vue 3 SPA
```

## License

[MIT](LICENSE).
