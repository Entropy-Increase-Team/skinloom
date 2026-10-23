import path from 'node:path'

import type { NextConfig } from 'next'
import createNextIntlPlugin from 'next-intl/plugin'

const withNextIntl = createNextIntlPlugin('./src/i18n/request.ts')

const config: NextConfig = {
  // Self-contained server.js for the release bundle and Docker images.
  output: 'standalone',
  // Trace dependencies from the pnpm workspace root, not just this app.
  outputFileTracingRoot: path.resolve(process.cwd(), '../..'),
  turbopack: { root: path.resolve(process.cwd(), '../..') },
  // Skins and avatars are rendered and cached by the Go backend.
  images: { unoptimized: true },
  transpilePackages: ['@skinloom/ui', '@skinloom/api-client'],
  poweredByHeader: false,
  reactStrictMode: true,
}

export default withNextIntl(config)
