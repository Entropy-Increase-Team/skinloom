# SkinLoom frontend only (Next.js standalone). It calls the backend at
# SKINLOOM_API_INTERNAL for server-side rendering; the backend gateway (or your ingress)
# routes page requests here. Build from the repository root:
#   docker build -f deploy/docker/web.Dockerfile -t skinloom/web .
# Runs per target platform: native modules in the standalone output must match it.
FROM node:24-alpine AS build
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
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 HOSTNAME=0.0.0.0 PORT=17547 \
    SKINLOOM_API_INTERNAL=http://server:17546
COPY --from=build /src/web/apps/web/.next/standalone ./
COPY --from=build /src/web/apps/web/.next/static ./apps/web/.next/static
USER node
EXPOSE 17547
# Pages render even while the backend is down (offline banner), so any answer below 500
# means the frontend process itself is healthy.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s \
  CMD ["node", "-e", "fetch('http://127.0.0.1:' + process.env.PORT + '/').then(r => process.exit(r.status < 500 ? 0 : 1), () => process.exit(1))"]
CMD ["node", "apps/web/server.js"]
