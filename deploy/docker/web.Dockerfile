# SkinLoom frontend only (Next.js standalone). It calls the backend at
# SKINLOOM_API_INTERNAL for server-side rendering; the backend gateway (or your ingress)
# routes page requests here. Build from the repository root:
#   docker build -f deploy/docker/web.Dockerfile -t skinloom/web .
FROM node:22-alpine AS build
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml web/.npmrc ./
COPY web/apps/web/package.json apps/web/
COPY web/packages/ui/package.json packages/ui/
COPY web/packages/api-client/package.json packages/api-client/
RUN pnpm install --frozen-lockfile
COPY api/openapi.json /src/api/openapi.json
COPY web/ ./
ENV NEXT_TELEMETRY_DISABLED=1
RUN pnpm build

FROM node:22-alpine
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 HOSTNAME=0.0.0.0 PORT=3000 \
    SKINLOOM_API_INTERNAL=http://server:8080
COPY --from=build /src/web/apps/web/.next/standalone ./
COPY --from=build /src/web/apps/web/.next/static ./apps/web/.next/static
USER node
EXPOSE 3000
CMD ["node", "apps/web/server.js"]
