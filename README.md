# OrionDrive

Self-hosted file management platform. Go backend, Vue 3 frontend.

OrionDrive is a clean-room project inspired by the feature set of self-hosted drives
(multi-backend storage, sharing, WebDAV, previews, search, admin). Authentication is
**OIDC / SSO only** — no local password accounts.

> Not affiliated with, and containing no code from, any GPL-licensed project. Licensed under MIT.

## Status

Milestone 0 (walking skeleton): single binary serving an embedded Vue SPA, OIDC login,
and a local-storage file explorer (list, folders, resumable upload, rename/move, trash,
download, quota) with the "Nebula" design system.

## Stack

- **Backend**: Go 1.26, Gin, GORM (SQLite default), cobra, coreos/go-oidc
- **Frontend**: Vue 3 + Vite + TypeScript + Pinia + shadcn-vue (Reka UI + Tailwind) + vue-i18n

## Development

```bash
# 1. Configure (copy and edit OIDC credentials)
cp conf.ini.example conf.ini

# 2. Backend
go run . migrate        # create the database schema
go run . server         # start API + embedded SPA on :5212

# 3. Frontend (dev, with hot reload proxying the API)
cd frontend
npm install
npm run dev
```

## Production build

```bash
cd frontend && npm run build     # outputs to frontend/dist, embedded by the backend
cd .. && go build -o orion-drive .
./orion-drive server
```

## Layout

```
cmd/            CLI commands (server, migrate)
conf/           configuration (INI + env overrides)
application/    bootstrap (DI) and embedded statics
model/          GORM models
repository/     data-access layer
pkg/            auth, cache, serializer, filemanager (driver/fs/chunk)
middleware/     Gin middleware
routers/        HTTP routes and controllers
service/        business logic
frontend/       Vue 3 SPA
```
