# SkinLoom backend only (API, game protocols, workers). Pair with skinloom/web, or use
# the all-in-one image. Build from the repository root:
#   docker build -f deploy/docker/server.Dockerfile -t skinloom/server .
FROM golang:1.27-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=0.0.0-dev
RUN go build -trimpath \
      -ldflags "-s -w -X github.com/Entropy-Increase-Team/skinloom/internal/buildinfo.Version=${VERSION}" \
      -o /out/skinloom ./cmd/skinloom \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/skinloom /usr/local/bin/skinloom
# distroless has no shell to chown at runtime: ship /data owned by the nonroot user,
# otherwise a fresh volume is root-owned and the first start cannot write to it.
COPY --from=build --chown=65532:65532 /out/data /data
ENV SKINLOOM_DATA=/data \
    SKINLOOM_WEB__MODE=off
VOLUME ["/data"]
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/skinloom"]
CMD ["serve"]
