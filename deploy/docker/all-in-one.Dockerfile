# SkinLoom all-in-one: backend + frontend in one container via `skinloom up`, the same
# layout as the desktop release bundles. Simplest way to run a site:
#   docker build -f deploy/docker/all-in-one.Dockerfile -t skinloom/skinloom .
#   docker run -d -p 8080:8080 -v skinloom-data:/data skinloom/skinloom
FROM golang:1.27-alpine AS server
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=0.0.0-dev
RUN go build -trimpath \
      -ldflags "-s -w -X github.com/Entropy-Increase-Team/skinloom/internal/buildinfo.Version=${VERSION}" \
      -o /out/skinloom ./cmd/skinloom

FROM node:24-alpine AS web
WORKDIR /src/web
# bun installs exactly what web/bun.lock pins; Next.js itself builds and runs on Node.
COPY --from=oven/bun:1.3-alpine /usr/local/bin/bun /usr/local/bin/bun
COPY web/package.json web/bun.lock web/bunfig.toml ./
COPY web/apps/web/package.json apps/web/
COPY web/packages/ui/package.json packages/ui/
COPY web/packages/api-client/package.json packages/api-client/
RUN bun install --frozen-lockfile
COPY web/ ./
ENV NEXT_TELEMETRY_DISABLED=1
RUN bun run build

FROM node:24-alpine
WORKDIR /app
COPY --from=server /out/skinloom /app/skinloom
COPY --from=web /src/web/apps/web/.next/standalone /app/web
COPY --from=web /src/web/apps/web/.next/static /app/web/apps/web/.next/static
RUN mkdir -p /data && chown node:node /data
ENV SKINLOOM_DATA=/data SKINLOOM_WEB__DIR=/app/web NEXT_TELEMETRY_DISABLED=1
VOLUME ["/data"]
EXPOSE 8080
USER node
ENTRYPOINT ["/app/skinloom"]
CMD ["up"]
