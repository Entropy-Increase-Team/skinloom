#!/usr/bin/env node
// Reusable Playwright smoke, accessibility and payload measurement script (design 07 §8 / P5).
//
// Usage:
//   node scripts/smoke.mjs [--url http://localhost:18080] [--widths 375,768,1280]
//     [--themes default,brocade] [--modes light,dark] [--routes /,/skinlib,/about,/auth/login,/auth/register]
//     [--auth-routes /user,/admin/appearance] [--email ... --password ...]
//     [--admin-token ...] [--shots ./tmp/smoke-shots] [--measure]
//
// Playwright resolution: uses `playwright` or `playwright-core` if resolvable, or
// PLAYWRIGHT_MODULE=<path-to-playwright-core/index.mjs>. Keeps CI install lean without
// adding browser binaries to web/bun.lock.
import { existsSync, mkdirSync, readdirSync } from 'node:fs'
import { createRequire } from 'node:module'
import path from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const DEFAULT_WIDTHS = [375, 768, 1280]
const DEFAULT_MODES = ['light', 'dark']
const DEFAULT_ROUTES = ['/', '/skinlib', '/about', '/auth/login', '/auth/register']

export function parseArgs(argv) {
  const opts = {
    url: process.env.SKINLOOM_URL ?? 'http://localhost:18080',
    widths: DEFAULT_WIDTHS,
    themes: null,
    modes: DEFAULT_MODES,
    routes: DEFAULT_ROUTES,
    authRoutes: [],
    email: '',
    password: '',
    adminToken: process.env.SKINLOOM_ADMIN_TOKEN ?? '',
    notFoundRoute: '',
    shots: '',
    measure: false,
  }
  const csv = (v) => v.split(',').map((s) => s.trim()).filter(Boolean)
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i]
    const next = () => {
      if (i + 1 >= argv.length) throw new Error(`missing value for ${a}`)
      return argv[++i]
    }
    switch (a) {
      case '--url':
        opts.url = next().replace(/\/+$/, '')
        break
      case '--widths':
        opts.widths = csv(next()).map((n) => Number(n))
        break
      case '--themes':
        opts.themes = csv(next())
        break
      case '--modes':
        opts.modes = csv(next())
        break
      case '--routes':
        opts.routes = csv(next())
        break
      case '--auth-routes':
        opts.authRoutes = csv(next())
        break
      case '--email':
        opts.email = next()
        break
      case '--password':
        opts.password = next()
        break
      case '--admin-token':
        opts.adminToken = next()
        break
      case '--not-found':
        opts.notFoundRoute = next()
        break
      case '--shots':
        opts.shots = next()
        break
      case '--measure':
        opts.measure = true
        break
      default:
        throw new Error(`unknown flag: ${a}`)
    }
  }
  let parsedUrl
  try {
    parsedUrl = new URL(opts.url)
  } catch {
    throw new Error(`invalid --url: ${opts.url}`)
  }
  if (parsedUrl.protocol !== 'http:' && parsedUrl.protocol !== 'https:') {
    throw new Error(`invalid --url protocol: ${parsedUrl.protocol}`)
  }
  if (opts.widths.length === 0 || opts.widths.some((w) => !Number.isInteger(w) || w <= 0)) {
    throw new Error(`invalid --widths: ${opts.widths.join(',')}`)
  }
  if (opts.modes.length === 0 || opts.modes.some((m) => m !== 'light' && m !== 'dark')) {
    throw new Error(`invalid --modes (expected light,dark): ${opts.modes.join(',')}`)
  }
  return opts
}

export function routeSlug(route) {
  return route === '/' ? 'home' : route.replace(/^\/+/, '').replace(/[^a-zA-Z0-9_-]+/g, '_')
}

async function loadPlaywright() {
  const candidates = []
  if (process.env.PLAYWRIGHT_MODULE) candidates.push(process.env.PLAYWRIGHT_MODULE)
  const req = createRequire(import.meta.url)
  for (const pkg of ['playwright', 'playwright-core']) {
    try {
      candidates.push(req.resolve(pkg))
    } catch {
      // not installed in workspace node_modules
    }
  }
  for (const c of candidates) {
    try {
      const mod = await import(pathToFileURL(c).href)
      if (mod.chromium) return mod
      if (mod.default?.chromium) return mod.default
    } catch {
      // try next candidate
    }
  }
  throw new Error(
    'playwright / playwright-core not found. Set PLAYWRIGHT_MODULE=/path/to/playwright-core/index.mjs or install playwright-core.',
  )
}

function findChromiumExecutable(playwright) {
  if (process.env.SKINLOOM_BROWSER && existsSync(process.env.SKINLOOM_BROWSER)) {
    return process.env.SKINLOOM_BROWSER
  }
  try {
    const p = playwright.chromium.executablePath()
    if (p && existsSync(p)) return p
  } catch {
    // check known caches/browsers
  }
  const localApp = process.env.LOCALAPPDATA
  if (localApp) {
    const cacheDir = path.join(localApp, 'ms-playwright')
    if (existsSync(cacheDir)) {
      const dirs = readdirSync(cacheDir).filter((d) => d.startsWith('chromium-')).sort().reverse()
      for (const d of dirs) {
        const exe = path.join(cacheDir, d, 'chrome-win64', 'chrome.exe')
        if (existsSync(exe)) return exe
      }
    }
  }
  for (const sys of [
    'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
    'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe',
    '/usr/bin/chromium',
    '/usr/bin/google-chrome',
  ]) {
    if (existsSync(sys)) return sys
  }
  return undefined
}

async function getActiveTheme(baseUrl) {
  const res = await fetch(`${baseUrl}/api/v1/site/bootstrap`)
  if (!res.ok) throw new Error(`GET /api/v1/site/bootstrap failed: ${res.status}`)
  const data = await res.json()
  return data.theme?.id ?? 'default'
}

async function setActiveTheme(baseUrl, adminToken, themeId) {
  const res = await fetch(`${baseUrl}/api/v1/admin/settings`, {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${adminToken}`,
    },
    body: JSON.stringify({ 'theme.active': themeId }),
  })
  if (!res.ok) {
    throw new Error(`PATCH /api/v1/admin/settings theme.active=${themeId} failed: ${res.status}`)
  }
}

async function inspectPage(page, width) {
  return page.evaluate((vw) => {
    const doc = document.documentElement
    const scrollW = doc.scrollWidth
    const clientW = doc.clientWidth
    const overflowEls = []
    for (const el of document.querySelectorAll('body *')) {
      const style = getComputedStyle(el)
      if (style.display === 'none' || style.visibility === 'hidden') continue
      const r = el.getBoundingClientRect()
      if (r.width === 0 && r.height === 0) continue
      if (r.right > clientW + 1.5 || r.left < -1.5) {
        // Ignore children legitimately inside an overflow-x scroll container (e.g. tables, mobile tabs).
        let clipped = false
        for (let p = el.parentElement; p; p = p.parentElement) {
          const ps = getComputedStyle(p)
          if (ps.overflowX === 'auto' || ps.overflowX === 'scroll' || ps.overflowX === 'hidden') {
            const pr = p.getBoundingClientRect()
            if (pr.right <= clientW + 1.5 && pr.left >= -1.5) {
              clipped = true
              break
            }
          }
        }
        if (!clipped) {
          overflowEls.push(`${el.tagName.toLowerCase()}.${String(el.className).slice(0, 40)}(left=${r.left.toFixed(1)},right=${r.right.toFixed(1)})`)
        }
      }
    }

    const mains = document.querySelectorAll('main').length
    const skipLink = Boolean(document.querySelector('a[href="#main"]'))
    const h1Count = document.querySelectorAll('h1').length
    const unlabeledNavs = [...document.querySelectorAll('nav')]
      .filter((n) => n.getClientRects().length > 0 && getComputedStyle(n).visibility !== 'hidden' && !n.getAttribute('aria-label') && !n.getAttribute('aria-labelledby'))
      .map((n) => String(n.className).slice(0, 40))
    const unlabeledMeters = [...document.querySelectorAll('[role="meter"]')]
      .filter((m) => !m.getAttribute('aria-label') && !m.getAttribute('aria-labelledby'))
      .length

    const viewers = [...document.querySelectorAll('[data-slot="skin-viewer"]')]
    const viewerContainerHasImg = viewers.some((v) => v.getAttribute('role') === 'img')
    const viewerCanvasMissingRole = viewers.some((v) => {
      const c = v.querySelector('canvas')
      return c && (c.getAttribute('role') !== 'img' || !c.getAttribute('aria-label'))
    })

    const header = document.querySelector('header.sl-header')
    const headerRight = header ? header.getBoundingClientRect().right : 0

    return {
      theme: doc.getAttribute('data-sl-theme'),
      isDark: doc.classList.contains('dark'),
      vw,
      clientW,
      scrollW,
      horizontalOverflow: scrollW > clientW,
      overflowEls: overflowEls.slice(0, 5),
      headerOverflow: headerRight > clientW + 1,
      mains,
      skipLink,
      h1Count,
      unlabeledNavs,
      unlabeledMeters,
      viewerContainerHasImg,
      viewerCanvasMissingRole,
    }
  }, width)
}

async function measureRoute(browser, baseUrl, route) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } })
  const resources = []
  const failedAssets = []
  let timingSummary = []
  try {
    const page = await ctx.newPage()
    page.on('response', async (res) => {
      try {
        const url = res.url()
        if (!url.startsWith(baseUrl)) return
        const req = res.request()
        const type = req.resourceType()
        if (type !== 'script' && type !== 'stylesheet' && type !== 'font') return
        if (res.status() >= 400) {
          failedAssets.push(`${res.status()} ${url.slice(baseUrl.length)}`)
          return
        }
        const headers = res.headers()
        const cl = Number(headers['content-length'])
        let bytes = Number.isFinite(cl) && cl > 0 ? cl : 0
        if (!bytes) {
          const buf = await res.body().catch(() => null)
          bytes = buf ? buf.byteLength : 0
        }
        resources.push({ fullUrl: url, url: url.slice(baseUrl.length), type, bytes })
      } catch {
        // ignore aborted responses
      }
    })
    const docRes = await page.goto(`${baseUrl}${route}`, { waitUntil: 'load', timeout: 30000 })
    if (!docRes || docRes.status() >= 400) {
      throw new Error(`measureRoute ${route} returned status=${docRes?.status()}`)
    }
    await page.evaluate(() => document.fonts.ready)
    await page.waitForTimeout(500)
    timingSummary = await page.evaluate(() => {
      return performance.getEntriesByType('resource').map((e) => ({
        name: e.name,
        initiatorType: e.initiatorType,
        transferSize: e.transferSize,
        encodedBodySize: e.encodedBodySize,
        decodedBodySize: e.decodedBodySize,
      }))
    })
  } finally {
    await ctx.close()
  }
  if (failedAssets.length > 0) {
    throw new Error(`measureRoute ${route} failed asset(s): ${failedAssets.slice(0, 5).join(', ')}`)
  }

  const sumBy = (pred) =>
    resources.filter(pred).reduce(
      (acc, r) => {
        const perf = timingSummary.find((p) => p.name === r.fullUrl)
        acc.count += 1
        acc.encoded += perf?.encodedBodySize || r.bytes
        acc.decoded += perf?.decodedBodySize || r.bytes
        return acc
      },
      { count: 0, encoded: 0, decoded: 0 },
    )

  return {
    route,
    js: sumBy((r) => r.type === 'script'),
    css: sumBy((r) => r.type === 'stylesheet'),
    fontsAll: sumBy((r) => r.type === 'font'),
    fontsNoto: sumBy((r) => r.type === 'font' && r.url.includes('noto-sans-sc')),
  }
}

async function verifyInteractive(page, route, width, kind) {
  const problems = []
  if (route === '/' || route === '/user') {
    await page.keyboard.press('Tab')
    const focused = await page.evaluate(() => {
      const a = document.activeElement
      if (!a || a.tagName !== 'A' || a.getAttribute('href') !== '#main') return null
      const r = a.getBoundingClientRect()
      return { width: r.width, height: r.height, left: r.left, right: r.right, top: r.top }
    })
    if (!focused || focused.width <= 0 || focused.height <= 0 || focused.left < 0 || focused.right > width) {
      problems.push(`skip link did not become visible on first Tab (${JSON.stringify(focused)})`)
    } else {
      await page.keyboard.press('Enter')
      const targetId = await page.evaluate(() => document.activeElement?.id ?? '')
      if (targetId !== 'main') {
        problems.push(`skip link Enter focused #${targetId || 'none'} instead of #main`)
      }
    }
  }
  if (width < 768 && ((route === '/' && kind === 'guest') || (route === '/user' && kind === 'auth'))) {
    const trigger = page.locator('header.sl-header button[aria-label]').locator('visible=true').first()
    if ((await trigger.count()) > 0) {
      await trigger.click()
      const dialog = page.locator('[role="dialog"]').first()
      await dialog.waitFor({ state: 'visible', timeout: 5000 })
      const sheetReport = await page.evaluate(() => {
        const d = document.querySelector('[role="dialog"]')
        const navs = d ? [...d.querySelectorAll('nav')].filter((n) => !n.getAttribute('aria-label') && !n.getAttribute('aria-labelledby')).length : 0
        return {
          unlabeledDialogNavs: navs,
          currentLinks: d ? d.querySelectorAll('[aria-current="page"]').length : 0,
          overflow: document.documentElement.scrollWidth > document.documentElement.clientWidth,
        }
      })
      if (sheetReport.unlabeledDialogNavs > 0) problems.push('mobile drawer nav missing aria-label')
      if (kind === 'auth' && sheetReport.currentLinks < 1) problems.push('sidebar nav missing aria-current="page"')
      if (sheetReport.overflow) problems.push('mobile drawer opened horizontal overflow')
      await page.keyboard.press('Escape')
      await dialog.waitFor({ state: 'hidden', timeout: 5000 })
    }
  }
  return problems
}

async function run() {
  const opts = parseArgs(process.argv.slice(2))
  const playwright = await loadPlaywright()
  const executablePath = findChromiumExecutable(playwright)
  const originalTheme = await getActiveTheme(opts.url)
  const themes = opts.themes ?? [originalTheme]
  const browser = await playwright.chromium.launch({ headless: true, executablePath })
  const failures = []
  let checked = 0

  try {
    if (opts.shots) mkdirSync(opts.shots, { recursive: true })

    if (opts.measure) {
      console.log('\n=== First-Screen Payload Audit (1280x800, fresh browser context) ===')
      for (const route of opts.routes) {
        const m = await measureRoute(browser, opts.url, route)
        const kb = (b) => `${(b / 1024).toFixed(1)} KB`
        console.log(
          `${route.padEnd(22)} JS: ${m.js.count} files, wire=${kb(m.js.encoded)}, decoded=${kb(m.js.decoded)} | CSS: ${kb(m.css.decoded)} | Fonts: ${m.fontsAll.count} files (${kb(m.fontsAll.encoded)}), NotoSC: ${m.fontsNoto.count} files (${kb(m.fontsNoto.encoded)})`,
        )
      }
    }

    for (const theme of themes) {
      const currentTheme = await getActiveTheme(opts.url)
      if (currentTheme !== theme) {
        if (!opts.adminToken) {
          throw new Error(`cannot switch theme from ${currentTheme} to ${theme} without --admin-token`)
        }
        await setActiveTheme(opts.url, opts.adminToken, theme)
      }

      // Sign in once per theme if authenticated routes + credentials are given.
      let authStorageState = null
      if (opts.authRoutes.length > 0 && opts.email && opts.password) {
        const loginCtx = await browser.newContext({ viewport: { width: 1280, height: 800 } })
        const loginPage = await loginCtx.newPage()
        await loginPage.goto(`${opts.url}/auth/login`, { waitUntil: 'load', timeout: 30000 })
        await loginPage.fill('input[name="email"]', opts.email)
        await loginPage.fill('input[name="password"]', opts.password)
        await Promise.all([loginPage.waitForURL((u) => !u.pathname.startsWith('/auth/login'), { timeout: 15000 }), loginPage.click('button[type="submit"]')])
        authStorageState = await loginCtx.storageState()
        await loginCtx.close()
      }

      const guestRoutes = opts.notFoundRoute ? [...opts.routes, opts.notFoundRoute] : opts.routes
      const batches = [
        { kind: 'guest', routes: guestRoutes, storageState: undefined },
        ...(authStorageState ? [{ kind: 'auth', routes: opts.authRoutes, storageState: authStorageState }] : []),
      ]

      for (const mode of opts.modes) {
        for (const width of opts.widths) {
          const height = width < 640 ? 812 : width < 1024 ? 1024 : 800
          for (const batch of batches) {
            const ctx = await browser.newContext({
              viewport: { width, height },
              colorScheme: mode,
              storageState: batch.storageState,
            })
            await ctx.addInitScript((m) => {
              localStorage.setItem('theme', m)
            }, mode)
            const page = await ctx.newPage()
            const errors = []
            let expectNotFoundUrl = ''
            page.on('console', (msg) => {
              if (msg.type() !== 'error') return
              const txt = msg.text()
              if (expectNotFoundUrl && txt.includes('404')) return
              errors.push(`console.error: ${txt}`)
            })
            page.on('pageerror', (err) => errors.push(`pageerror: ${err.message}`))
            page.on('response', (res) => {
              const s = res.status()
              if (s >= 400 && !res.url().includes('/favicon') && res.url() !== expectNotFoundUrl) {
                errors.push(`HTTP ${s} ${res.url()}`)
              }
            })

            for (const route of batch.routes) {
              errors.length = 0
              const isNotFound = route === opts.notFoundRoute
              expectNotFoundUrl = isNotFound ? `${opts.url}${route}` : ''
              const res = await page.goto(`${opts.url}${route}`, { waitUntil: 'load', timeout: 30000 })
              await page.evaluate(() => document.fonts.ready)
              await page.waitForTimeout(300)
              const report = await inspectPage(page, width)
              checked += 1

              const problems = []
              if (isNotFound) {
                if (res?.status() !== 404) problems.push(`status=${res?.status()} want 404`)
              } else if (!res || res.status() >= 400) {
                problems.push(`status=${res?.status()}`)
              }
              if (report.theme !== theme) problems.push(`theme=${report.theme} want ${theme}`)
              if (report.horizontalOverflow) problems.push(`scrollWidth ${report.scrollW} > clientWidth ${report.clientW}: ${report.overflowEls.join(', ')}`)
              if (report.headerOverflow) problems.push('header overflows viewport')
              if (report.mains !== 1) problems.push(`main count=${report.mains}, want 1`)
              if (!report.skipLink) problems.push('missing skip link a[href="#main"]')
              if (report.h1Count < 1) problems.push('missing h1 heading')
              if (report.unlabeledNavs.length > 0) problems.push(`unlabeled nav: ${report.unlabeledNavs.join(', ')}`)
              if (report.unlabeledMeters > 0) problems.push(`unlabeled meters: ${report.unlabeledMeters}`)
              if (report.viewerContainerHasImg) problems.push('skin-viewer outer div still has role="img"')
              if (report.viewerCanvasMissingRole) problems.push('skin-viewer canvas missing role="img"/aria-label')
              if (route === '/' || route === '/user') {
                problems.push(...(await verifyInteractive(page, route, width, batch.kind)))
              }
              if (errors.length > 0) problems.push(...errors)

              if (opts.shots) {
                const file = `${theme}-${mode}-${width}-${routeSlug(route)}.png`
                await page.screenshot({ path: path.join(opts.shots, file) })
              }
              if (problems.length > 0) {
                failures.push(`[${theme} / ${mode} / ${width}px] ${route}: ${problems.join(' ; ')}`)
                console.error(`FAIL [${theme} / ${mode} / ${width}px] ${route}: ${problems.join(' ; ')}`)
              } else {
                console.log(`PASS [${theme} / ${mode} / ${width}px] ${route} (scroll=${report.scrollW}/${report.clientW})`)
              }
            }
            await ctx.close()
          }
        }
      }
    }
  } finally {
    if (opts.adminToken) {
      const cur = await getActiveTheme(opts.url).catch(() => originalTheme)
      if (cur !== originalTheme) {
        await setActiveTheme(opts.url, opts.adminToken, originalTheme).catch(() => undefined)
      }
    }
    await browser.close()
  }

  console.log(`\nChecked ${checked} view(s); failures: ${failures.length}`)
  if (failures.length > 0) process.exit(1)
}

if (path.resolve(process.argv[1] ?? '') === fileURLToPath(import.meta.url)) {
  run().catch((err) => {
    console.error(err)
    process.exit(1)
  })
}
