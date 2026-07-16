# OrionDrive

[![CI](https://git.nhsoul.fr/nhpro/orion-drive/actions/workflows/ci.yaml/badge.svg)](https://git.nhsoul.fr/nhpro/orion-drive/actions)

Self-hosted file management platform. Go backend, Vue 3 frontend.

OrionDrive is a clean-room project inspired by the feature set of self-hosted drives
(multi-backend storage, sharing, WebDAV, previews, search, admin). Authentication is
**OIDC / SSO only** — no local password accounts.

> Not affiliated with, and containing no code from, any GPL-licensed project. Licensed under MIT.

## Status

Single binary serving an embedded Vue SPA with the "Nebula" design system. Implemented:

- **Files**: local storage explorer — list, folders, resumable chunked upload, rename/move,
  trash & restore, download, per-group quota.
- **Storage backends**: local, S3-compatible, and remote (slave node) drivers; direct/presigned
  downloads with per-group speed limiting.
- **Advanced files**: versioning, locking, AES-256-CTR at-rest encryption, direct links.
- **Archives**: online compress/extract (zip/tar/7z) as background tasks, grouped ZIP download.
- **Sharing**: file & folder links with password, expiry and download limits.
- **Preview & editing**: image/video/audio/PDF/text/Markdown/ePub; image editor; Office via WOPI
  (OnlyOffice / Collabora).
- **WebDAV**: mount your drive over `/dav` with dedicated per-user credentials (read-only option),
  independent of the SSO login.
- **Admin & organisation**: admin panel, groups with granular permissions and per-group storage
  policies, SSO group → group/admin mapping, scheduled maintenance (trash auto-purge, upload
  cleanup).
- **Databases & cache**: SQLite (default), PostgreSQL or MySQL; optional Redis-backed shared cache
  for multi-node deployments. Docker Compose ships all three behind opt-in profiles.

Not yet done: metadata/full-text search, PWA/i18n, master↔slave cluster orchestration,
SMTP/email notifications.

## Stack

- **Backend**: Go 1.26, Gin, GORM (SQLite / PostgreSQL / MySQL, all pure-Go / `CGO_ENABLED=0`),
  goose migrations, cobra, coreos/go-oidc, `golang.org/x/net/webdav`, optional Redis cache
- **Frontend**: Vue 3 + Vite + TypeScript + Pinia + vue-router, a custom "Nebula" CSS design system
  (oklch tokens, dark/light), lucide icons

## Development

```bash
# 1. Configure (copy and edit OIDC credentials)
cp conf.ini.example conf.ini

# 2. Backend
go run . migrate        # apply DB migrations (goose); `server` also runs these on startup
go run . server         # start API + embedded SPA on :5212

# 3. Frontend (dev, with hot reload proxying the API)
cd frontend
npm install
npm run dev
```

## Production build

```bash
cd frontend && npm run build     # outputs to application/statics/dist, embedded by the backend
cd .. && go build -o orion-drive .
./orion-drive server
```

## Docker

The multi-stage `Dockerfile` builds the frontend, compiles a static Go binary with
the SPA embedded, and ships a minimal Alpine runtime (with `ffmpeg` for thumbnails).

```bash
# Run the published image with compose (pulls it; data persists in the volume):
docker compose up -d             # http://localhost:5212

# Or plain docker with the published image:
docker pull git.nhsoul.fr/nhpro/orion-drive:latest
docker run -p 5212:5212 -v orion-data:/app/data \
  -e OD_CONF_System_SessionSecret="$(openssl rand -hex 32)" \
  git.nhsoul.fr/nhpro/orion-drive:latest

# Build locally from source instead:
docker build -t orion-drive .
```

Configure via `OD_CONF_<Section>_<Key>` environment variables (see `conf.ini.example`).
Set at least `OD_CONF_System_SessionSecret` and the `OD_CONF_OIDC_*` values for real logins;
the image runs in `release` mode, so the debug dev-login is disabled.

## Layout

```
cmd/            CLI commands (server, migrate, version, group, policy)
conf/           configuration (INI + env overrides)
application/    bootstrap (DI) and embedded statics
migrations/     goose SQL migrations (embedded)
model/          GORM models
repository/     data-access layer
pkg/            auth, cache, serializer, queue, crontab, filemanager (driver/encrypt),
                archive, thumb, wopi, webdav
middleware/     Gin middleware
routers/        HTTP routes and controllers
service/        business logic
frontend/       Vue 3 SPA
```
