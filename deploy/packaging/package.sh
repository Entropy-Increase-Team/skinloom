#!/usr/bin/env bash
# Build release bundles: skinloom binary + Next.js standalone frontend + portable Node.js
# + start scripts, one archive per target.
#
#   deploy/packaging/package.sh                          # current platform
#   deploy/packaging/package.sh linux/amd64 windows/amd64 darwin/arm64
#
# Environment:
#   VERSION          version string (default: git describe, else 0.0.0-dev)
#   NODE_VERSION     Node.js LTS bundled into the archives (default below)
#   NODE_MIRROR      download base (default https://nodejs.org/dist;
#                    in mainland China https://npmmirror.com/mirrors/node is faster)
#   NODE_BINARY      use this local node executable instead of downloading (current-platform builds only)
#   PACKAGE_MANAGER  bun | pnpm | npm for the frontend build (default: bun if installed,
#                    then pnpm, then npm; only bun installs from the committed bun.lock)
#   SKIP_WEB_BUILD=1 reuse an existing web/apps/web/.next/standalone build
#
# Output: dist/skinloom-<version>-<os>-<arch>.{zip,tar.gz} and dist/checksums.txt
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

NODE_VERSION="${NODE_VERSION:-24.22.1}"
NODE_MIRROR="${NODE_MIRROR:-https://nodejs.org/dist}"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || true)}"
VERSION="${VERSION:-0.0.0-dev}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || true)"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
BUILD="$ROOT/build"
DIST="$ROOT/dist"

host_target() {
  local os arch
  os="$(go env GOOS)"; arch="$(go env GOARCH)"
  echo "$os/$arch"
}

targets=("$@")
[ ${#targets[@]} -eq 0 ] && targets=("$(host_target)")

mkdir -p "$BUILD" "$DIST"

# ---- frontend (platform independent JavaScript) ---------------------------------------
if [ "${SKIP_WEB_BUILD:-}" != "1" ]; then
  pm="${PACKAGE_MANAGER:-}"
  if [ -z "$pm" ]; then
    for cand in bun pnpm npm; do command -v "$cand" >/dev/null && { pm="$cand"; break; }; done
  fi
  echo "==> building frontend with $pm"
  case "$pm" in
    bun)  (cd web && bun install --frozen-lockfile && bun run build) ;;
    pnpm) (cd web && pnpm install --no-frozen-lockfile && pnpm run build) ;;   # no committed pnpm lockfile (CI=true implies frozen)
    npm)  (cd web && npm install --no-audit --no-fund && npm run build) ;;
    *)    echo "no package manager found (bun, pnpm or npm)" >&2; exit 1 ;;
  esac
fi
STANDALONE="web/apps/web/.next/standalone"
[ -f "$STANDALONE/apps/web/server.js" ] || { echo "missing $STANDALONE/apps/web/server.js; build the frontend first" >&2; exit 1; }
echo "==> staging frontend"
rm -rf "$BUILD/web"
mkdir -p "$BUILD/web"
# -L: copy real files even if the package manager left symlinks, so archives work everywhere
cp -RL "$STANDALONE/." "$BUILD/web/"
# standalone output leaves static assets and public/ to the deployer
mkdir -p "$BUILD/web/apps/web/.next"
cp -R web/apps/web/.next/static "$BUILD/web/apps/web/.next/static"
[ -d web/apps/web/public ] && cp -R web/apps/web/public "$BUILD/web/apps/web/public"

# ---- per target ----------------------------------------------------------------------
sha256() { # file -> hex digest (sha256sum on Linux/Git Bash, shasum on macOS)
  if command -v sha256sum >/dev/null; then sha256sum "$1" | awk '{print $1}'
  else shasum -a 256 "$1" | awk '{print $1}'; fi
}

fetch_node() { # os arch dest
  local os="$1" arch="$2" dest="$3" nos narch ext name url
  case "$os" in windows) nos=win ;; darwin) nos=darwin ;; linux) nos=linux ;; *) echo "no node for $os" >&2; return 1 ;; esac
  case "$arch" in amd64) narch=x64 ;; arm64) narch=arm64 ;; *) echo "no node for $arch" >&2; return 1 ;; esac
  if [ -n "${NODE_BINARY:-}" ] && [ "$os/$arch" = "$(host_target)" ]; then
    mkdir -p "$dest"
    if [ "$os" = windows ]; then cp "$NODE_BINARY" "$dest/node.exe"; else mkdir -p "$dest/bin" && cp "$NODE_BINARY" "$dest/bin/node"; fi
    return 0
  fi
  name="node-v$NODE_VERSION-$nos-$narch"
  ext=tar.gz; [ "$os" = windows ] && ext=zip
  local cache="$BUILD/node-cache/$name.$ext"
  local sums="$BUILD/node-cache/SHASUMS256-v$NODE_VERSION.txt"
  mkdir -p "$BUILD/node-cache"
  if [ ! -f "$sums" ]; then
    # Checksums come from nodejs.org even when the archive comes from a mirror, so a
    # tampered mirror cannot ship a modified Node runtime inside our release bundles.
    curl -fsSL --retry 3 -o "$sums.part" "https://nodejs.org/dist/v$NODE_VERSION/SHASUMS256.txt" && mv "$sums.part" "$sums"
  fi
  if [ ! -f "$cache" ]; then
    url="$NODE_MIRROR/v$NODE_VERSION/$name.$ext"
    echo "    downloading $url"
    curl -fL --retry 3 -o "$cache.part" "$url" && mv "$cache.part" "$cache"
  fi
  local want got
  want="$(awk -v f="$name.$ext" '$2 == f { print $1 }' "$sums")"
  got="$(sha256 "$cache")"
  if [ -z "$want" ] || [ "$want" != "$got" ]; then
    rm -f "$cache"
    echo "checksum mismatch for $name.$ext (want ${want:-<not listed>}, got $got)" >&2
    return 1
  fi
  local tmp="$BUILD/node-extract/$name"
  rm -rf "$tmp"; mkdir -p "$tmp"
  if [ "$ext" = zip ]; then unzip -q "$cache" -d "$tmp"; else tar -xzf "$cache" -C "$tmp"; fi
  rm -rf "$dest"; mkdir -p "$dest"
  if [ "$os" = windows ]; then
    cp "$tmp/$name/node.exe" "$tmp/$name/LICENSE" "$dest/"
  else
    mkdir -p "$dest/bin"
    cp "$tmp/$name/bin/node" "$dest/bin/node"
    cp "$tmp/$name/LICENSE" "$dest/"
  fi
}

: > "$DIST/checksums.txt.new"
for target in "${targets[@]}"; do
  os="${target%/*}"; arch="${target#*/}"
  name="skinloom-$VERSION-$os-$arch"
  stage="$BUILD/stage/$name"
  echo "==> $target"
  rm -rf "$stage"; mkdir -p "$stage"

  exe=skinloom; [ "$os" = windows ] && exe=skinloom.exe
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
    -ldflags "-s -w -X github.com/Entropy-Increase-Team/skinloom/internal/buildinfo.Version=$VERSION -X github.com/Entropy-Increase-Team/skinloom/internal/buildinfo.Commit=$COMMIT -X github.com/Entropy-Increase-Team/skinloom/internal/buildinfo.Date=$DATE" \
    -o "$stage/$exe" ./cmd/skinloom

  cp -RL "$BUILD/web" "$stage/web"
  fetch_node "$os" "$arch" "$stage/runtime/node"
  cp LICENSE NOTICE "$stage/"
  cp deploy/packaging/README.txt "$stage/README.txt"
  case "$os" in
    windows) cp deploy/packaging/start.bat "$stage/" ;;
    darwin)  cp deploy/packaging/start.command "$stage/" && chmod +x "$stage/start.command" ;;
    *)       cp deploy/packaging/start.sh "$stage/" && chmod +x "$stage/start.sh" ;;
  esac

  (
    cd "$BUILD/stage"
    if [ "$os" = windows ]; then
      rm -f "$DIST/$name.zip"
      if command -v zip >/dev/null; then zip -qr "$DIST/$name.zip" "$name"
      else powershell.exe -NoProfile -Command "Compress-Archive -Path '$name' -DestinationPath '$(cygpath -w "$DIST/$name.zip" 2>/dev/null || echo "$DIST/$name.zip")'"; fi
      archive="$name.zip"
    else
      tar -czf "$DIST/$name.tar.gz" "$name"
      archive="$name.tar.gz"
    fi
    (cd "$DIST" && printf '%s  %s\n' "$(sha256 "$archive")" "$archive") >> "$DIST/checksums.txt.new"
  )
  echo "    -> dist/$( [ "$os" = windows ] && echo "$name.zip" || echo "$name.tar.gz")"
done
mv "$DIST/checksums.txt.new" "$DIST/checksums.txt"
echo "==> done; unpacked bundles are in build/stage/"
