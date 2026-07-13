# syntax=docker/dockerfile:1

# ---- Stage 1: build the Vue frontend (output goes to application/statics/dist) ----
FROM node:22-alpine AS frontend
WORKDIR /src/frontend
# Install dependencies first for better layer caching.
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
# Vite writes the build to ../application/statics/dist (embedded by the Go binary).
RUN npm run build

# ---- Stage 2: build the single Go binary with the SPA embedded ----
FROM golang:1.26-alpine AS build
WORKDIR /src
# Download modules first (cached until go.mod/go.sum change).
COPY go.mod go.sum ./
RUN go mod download
# Copy the source, then drop in the freshly built SPA before compiling (go:embed).
COPY . .
COPY --from=frontend /src/application/statics/dist ./application/statics/dist
# Stamp the version at build time: docker build --build-arg VERSION=v0.6.0
ARG VERSION=docker
ARG COMMIT=none
# Pure-Go build (glebarez/sqlite needs no CGO) → a static binary.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags "-s -w \
      -X github.com/NhProGamer/orion-drive/application/constants.Version=${VERSION} \
      -X github.com/NhProGamer/orion-drive/application/constants.Commit=${COMMIT}" \
    -o /orion-drive .

# ---- Stage 3: minimal runtime ----
FROM alpine:3.20
# ca-certificates for OIDC/S3 TLS; ffmpeg powers video/audio thumbnails; tzdata for local times.
RUN apk add --no-cache ca-certificates ffmpeg tzdata \
 && adduser -D -u 1000 orion \
 && mkdir -p /app/data \
 && chown -R orion:orion /app
WORKDIR /app
COPY --from=build /orion-drive /usr/local/bin/orion-drive

USER orion

# Sensible defaults; override any with OD_CONF_<Section>_<Key> env vars.
ENV OD_CONF_System_Mode=release \
    OD_CONF_System_Listen=:5212 \
    OD_CONF_Database_DBFile=/app/data/orion.db \
    OD_CONF_Storage_LocalBasePath=/app/data/storage

VOLUME ["/app/data"]
EXPOSE 5212

# `server` runs migrations then serves the API + embedded SPA on one port.
ENTRYPOINT ["orion-drive"]
CMD ["server"]
