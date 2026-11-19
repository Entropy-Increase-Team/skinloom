import { createHash, createPrivateKey, createPublicKey, generateKeyPairSync, sign, verify } from 'node:crypto'
import { chmodSync, existsSync, mkdirSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { buildPlugin } from './build.js'
import {
  effectiveRuntime,
  isNativeExecutablePath,
  loadManifest,
  MAX_ASSET_FILE_BYTES,
  MAX_NATIVE_BINARY_BYTES,
  MAX_NATIVE_FAT_PACKAGE_BYTES,
  MAX_NATIVE_SINGLE_PACKAGE_BYTES,
  MAX_NATIVE_UNPACKED_FAT_BYTES,
  MAX_NATIVE_UNPACKED_SINGLE_BYTES,
  MAX_PACKAGE_TOTAL_BYTES,
  MAX_SERVER_BUNDLE_BYTES,
  parseTargetFromBinaryPath,
  PLUGIN_ID_RE,
  THEME_ID_RE,
  walkFiles,
  type ManifestJson,
} from './manifest.js'
import { readZip, writeZip, type ZipEntryInput } from './zip.js'

export interface KeyPairFiles {
  publicKeyPem: string
  privateKeyPem: string
  publicKeyPath?: string
  privateKeyPath?: string
}

export interface SplitArtifactEntry {
  file: string
  archivePath: string
  sha256: string
  size: number
  signature?: string
}

export interface PackOptions {
  dir?: string
  buildDir?: string
  outFile?: string
  outDir?: string
  privateKeyPem?: string
  privateKeyFile?: string
  skipBuild?: boolean
  native?: boolean
  targets?: string[]
  splitPlatforms?: boolean
  fat?: boolean
  goBin?: string
}

export interface PackResult {
  archivePath: string
  archiveBytes: number
  fileCount: number
  checksums: string
  signed: boolean
  artifacts?: Record<string, SplitArtifactEntry>
  artifactsManifestPath?: string
}

export interface SignOptions {
  archivePath: string
  privateKeyPem?: string
  privateKeyFile?: string
  outPath?: string
}

export interface SignResult {
  archivePath: string
  checksums: string
  signatureBase64: string
}

export interface VerifyOptions {
  archivePath: string
  publicKeyPem?: string
  publicKeyFile?: string
}

export interface VerifyResult {
  ok: boolean
  errors: string[]
  fileCount: number
  signed: boolean
}

function resolveKey(pem?: string, filePath?: string, label = 'key'): string {
  if (pem && pem.trim() !== '') return pem
  if (filePath && filePath.trim() !== '') return readFileSync(path.resolve(filePath), 'utf8')
  throw new Error(`Missing Ed25519 ${label} (--key / privateKeyPem)`)
}

/** Generates an Ed25519 PKCS#8 + SPKI PEM key pair, optionally writing `<name>.{pub,key}.pem`. */
export function generateKeyPair(outDir?: string, name = 'signing', force = false): KeyPairFiles {
  if (/[/\\]/.test(name) || name.includes('..') || !name.trim()) {
    throw new Error(`Invalid key file base name "${name}"`)
  }
  const { publicKey, privateKey } = generateKeyPairSync('ed25519', {
    publicKeyEncoding: { type: 'spki', format: 'pem' },
    privateKeyEncoding: { type: 'pkcs8', format: 'pem' },
  })
  const result: KeyPairFiles = {
    publicKeyPem: publicKey,
    privateKeyPem: privateKey,
  }
  if (outDir) {
    const resolved = path.resolve(outDir)
    mkdirSync(resolved, { recursive: true })
    const pubPath = path.join(resolved, `${name}.pub.pem`)
    const privPath = path.join(resolved, `${name}.key.pem`)
    if (!force && (existsSync(pubPath) || existsSync(privPath))) {
      throw new Error(`Key pair already exists at ${privPath}; pass --force to overwrite`)
    }
    writeFileSync(pubPath, publicKey, 'utf8')
    writeFileSync(privPath, privateKey, { encoding: 'utf8', mode: 0o600 })
    try {
      chmodSync(privPath, 0o600)
    } catch {
      // ignore chmod errors on filesystems without POSIX permissions
    }
    result.publicKeyPath = pubPath
    result.privateKeyPath = privPath
  }
  return result
}

/** Computes sorted deterministic `sha256hex  relpath\n` lines over archive entries. */
export function computeChecksums(entries: Array<{ name: string; data: Uint8Array }>): string {
  const filtered = entries
    .filter((e) => e.name !== 'checksums.txt' && e.name !== 'signature')
    .sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0))
  const lines = filtered.map((e) => {
    const hex = createHash('sha256').update(e.data).digest('hex')
    return `${hex}  ${e.name}`
  })
  return lines.length > 0 ? `${lines.join('\n')}\n` : ''
}

function signChecksums(checksumsText: string, privateKeyPem: string): string {
  const keyObj = createPrivateKey(privateKeyPem)
  if (keyObj.asymmetricKeyType !== 'ed25519') {
    throw new Error(`Expected Ed25519 private key, got ${String(keyObj.asymmetricKeyType)}`)
  }
  const sig = sign(null, Buffer.from(checksumsText, 'utf8'), keyObj)
  return `${sig.toString('base64')}\n`
}

function validateManifestIdentityForFile(manifest: ManifestJson): void {
  const idOk =
    manifest.kind === 'theme'
      ? THEME_ID_RE.test(manifest.id ?? '') && manifest.id !== '_custom'
      : PLUGIN_ID_RE.test(manifest.id ?? '')
  if (!idOk) {
    throw new Error(`Invalid plugin/theme id "${manifest.id ?? ''}" in plugin.json`)
  }
  if (
    !manifest.version ||
    manifest.version.length > 64 ||
    !/^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$/.test(manifest.version) ||
    manifest.version.includes('..')
  ) {
    throw new Error(`Invalid version "${manifest.version ?? ''}" in plugin.json`)
  }
}

function resolveArchiveCaps(manifest: ManifestJson | undefined, isFat: boolean): { maxArchive: number; maxUnpacked: number } {
  if (manifest && effectiveRuntime(manifest) === 'native') {
    return isFat
      ? { maxArchive: MAX_NATIVE_FAT_PACKAGE_BYTES, maxUnpacked: MAX_NATIVE_UNPACKED_FAT_BYTES }
      : { maxArchive: MAX_NATIVE_SINGLE_PACKAGE_BYTES, maxUnpacked: MAX_NATIVE_UNPACKED_SINGLE_BYTES }
  }
  return { maxArchive: MAX_PACKAGE_TOTAL_BYTES, maxUnpacked: MAX_PACKAGE_TOTAL_BYTES }
}

function parseManifestFromEntries(entries: Array<{ name: string; data: Uint8Array }>): ManifestJson | undefined {
  const entry = entries.find((e) => e.name === 'plugin.json')
  if (!entry) return undefined
  try {
    return JSON.parse(Buffer.from(entry.data).toString('utf8')) as ManifestJson
  } catch {
    return undefined
  }
}

function isFatEntries(manifest: ManifestJson | undefined, entries: Array<{ name: string }>): boolean {
  if (!manifest || effectiveRuntime(manifest) !== 'native') return false
  if ((manifest.native?.platforms?.length ?? 0) > 1) return true
  let bins = 0
  for (const e of entries) {
    if (isNativeExecutablePath(manifest, e.name)) {
      bins++
      if (bins > 1) return true
    }
  }
  return false
}

function buildArchiveFromEntries(
  entries: ZipEntryInput[],
  manifest: ManifestJson,
  isFat: boolean,
  privPem?: string,
): { archiveBytes: Uint8Array; checksums: string; signatureBase64?: string; fileCount: number; sha256: string } {
  const { maxArchive, maxUnpacked } = resolveArchiveCaps(manifest, isFat)
  const isNative = effectiveRuntime(manifest) === 'native'
  const entryNames = new Set(entries.map((e) => e.name))
  if (!isNative && manifest.server) {
    const cleanServer = path.posix.normalize(manifest.server.replace(/^\.\//, ''))
    if (!entryNames.has(cleanServer)) {
      throw new Error(`Declared server entry "${manifest.server}" is missing from archive entries`)
    }
  }
  if (manifest.web) {
    const cleanWeb = path.posix.normalize(manifest.web.replace(/^\.\//, ''))
    if (!entryNames.has(cleanWeb)) {
      throw new Error(`Declared web entry "${manifest.web}" is missing from archive entries`)
    }
  }
  let totalRawBytes = 0
  const cleanServerName = manifest.server ? path.posix.normalize(manifest.server.replace(/^\.\//, '')) : ''
  const finalEntries: ZipEntryInput[] = []
  for (const e of entries) {
    const isNativeBin = isNative && isNativeExecutablePath(manifest, e.name)
    if (!isNative && cleanServerName && e.name === cleanServerName && e.data.byteLength > MAX_SERVER_BUNDLE_BYTES) {
      throw new Error(
        `Server bundle "${e.name}" (${e.data.byteLength} bytes) exceeds 5 MiB QuickJS sandbox limit`,
      )
    }
    const fileCap = isNativeBin ? MAX_NATIVE_BINARY_BYTES : MAX_ASSET_FILE_BYTES
    if (e.data.byteLength > fileCap) {
      throw new Error(`File "${e.name}" (${e.data.byteLength} bytes) exceeds ${Math.round(fileCap / (1024 * 1024))} MiB limit`)
    }
    totalRawBytes += e.data.byteLength
    if (totalRawBytes > maxUnpacked) {
      throw new Error(`Total unpacked size exceeds ${Math.round(maxUnpacked / (1024 * 1024))} MiB package cap`)
    }
    finalEntries.push({
      name: e.name,
      data: e.data,
      mode: e.mode ?? (isNativeBin ? 0o100755 : 0o100644),
    })
  }

  const checksums = computeChecksums(finalEntries)
  finalEntries.push({ name: 'checksums.txt', data: Buffer.from(checksums, 'utf8') })

  let signatureBase64: string | undefined
  if (privPem) {
    const sigText = signChecksums(checksums, privPem)
    signatureBase64 = sigText.trim()
    finalEntries.push({ name: 'signature', data: Buffer.from(sigText, 'utf8') })
  }

  const archiveBytes = writeZip(finalEntries)
  if (archiveBytes.byteLength > maxArchive) {
    throw new Error(
      `Packed archive (${archiveBytes.byteLength} bytes) exceeds ${Math.round(maxArchive / (1024 * 1024))} MiB package cap`,
    )
  }
  const sha256 = createHash('sha256').update(archiveBytes).digest('hex')
  return {
    archiveBytes,
    checksums,
    signatureBase64,
    fileCount: finalEntries.length,
    sha256,
  }
}

/**
 * Builds (unless `skipBuild`) and packages a plugin or theme into `.slp` archive(s).
 *
 * For Tier 2 `runtime: "native"` plugins:
 * - `splitPlatforms: true` emits one `<id>-<version>-<os>-<arch>.slp` per built
 *   platform binary plus `artifacts.json` (recommended for registry publishing).
 * - `fat: true` (or default when not split) emits one combined `.slp` containing
 *   all built binaries in `buildDir`.
 */
export async function packPlugin(options: PackOptions = {}): Promise<PackResult> {
  if (options.splitPlatforms && options.fat) {
    throw new Error('Cannot combine --split-platforms and --fat in the same pack invocation')
  }

  const projectDir = path.resolve(options.dir ?? '.')
  const defaultSkipBuildDir = existsSync(path.join(projectDir, 'dist', 'plugin.json')) ? 'dist' : '.'
  const buildDir = options.skipBuild
    ? path.resolve(projectDir, options.buildDir ?? defaultSkipBuildDir)
    : (
        await buildPlugin({
          dir: projectDir,
          outDir: options.buildDir ?? 'dist',
          minify: true,
          native: options.native,
          targets: options.targets,
          goBin: options.goBin,
          allowUncappedCombined: Boolean(options.splitPlatforms),
        })
      ).outDir

  const { manifest } = loadManifest(buildDir)
  validateManifestIdentityForFile(manifest)
  const isNative = effectiveRuntime(manifest) === 'native'
  const privPem =
    options.privateKeyPem || options.privateKeyFile
      ? resolveKey(options.privateKeyPem, options.privateKeyFile, 'private key')
      : undefined

  const relFiles = walkFiles(buildDir, new Set(['node_modules', '.git'])).filter(
    (rel) =>
      !rel.endsWith('.slp') &&
      !rel.endsWith('.key.pem') &&
      !rel.split('/').some((seg) => seg.startsWith('.')) &&
      rel !== 'checksums.txt' &&
      rel !== 'signature',
  )

  if (options.splitPlatforms) {
    if (!isNative || !manifest.native?.executable) {
      throw new Error('--split-platforms is only supported for runtime "native" plugins with native.executable')
    }
    const sharedEntries: ZipEntryInput[] = []
    const platformBinaries = new Map<string, ZipEntryInput>()

    for (const rel of relFiles) {
      if (rel === 'plugin.json') continue
      const abs = path.join(buildDir, ...rel.split('/'))
      const data = readFileSync(abs)
      if (isNativeExecutablePath(manifest, rel)) {
        const target = parseTargetFromBinaryPath(manifest.native.executable, rel)
        if (!target) {
          throw new Error(
            `Native binary "${rel}" does not carry an <os>_<arch> suffix required for --split-platforms`,
          )
        }
        if (platformBinaries.has(target)) {
          throw new Error(`Duplicate native binary for target "${target}" ("${rel}")`)
        }
        platformBinaries.set(target, { name: rel, data, mode: 0o100755 })
      } else {
        sharedEntries.push({ name: rel, data })
      }
    }

    const targets = Array.from(platformBinaries.keys()).sort()
    if (targets.length === 0) {
      throw new Error(`No platform-suffixed native binaries found under ${buildDir}`)
    }

    const outDir = path.resolve(projectDir, options.outDir ?? (options.outFile ? path.dirname(options.outFile) : '.'))
    mkdirSync(outDir, { recursive: true })

    const artifacts: Record<string, SplitArtifactEntry> = {}
    let firstArchivePath = ''
    let firstArchiveBytes = 0
    let firstFileCount = 0
    let firstChecksums = ''

    for (const target of targets) {
      const binEntry = platformBinaries.get(target)!
      const perManifest: ManifestJson = structuredClone(manifest)
      perManifest.native = {
        protocolVersion: 1,
        ...manifest.native,
        platforms: [target],
      }
      const perEntries: ZipEntryInput[] = [
        { name: 'plugin.json', data: Buffer.from(`${JSON.stringify(perManifest, null, 2)}\n`, 'utf8') },
        ...sharedEntries,
        binEntry,
      ]
      const built = buildArchiveFromEntries(perEntries, perManifest, false, privPem)
      const [goos, goarch] = target.split('/')
      const fileName = `${manifest.id}-${manifest.version}-${goos}-${goarch}.slp`
      const archivePath = path.join(outDir, fileName)
      writeFileSync(archivePath, built.archiveBytes)

      artifacts[target] = {
        file: fileName,
        archivePath,
        sha256: built.sha256,
        size: built.archiveBytes.byteLength,
        ...(built.signatureBase64 ? { signature: built.signatureBase64 } : {}),
      }
      if (!firstArchivePath) {
        firstArchivePath = archivePath
        firstArchiveBytes = built.archiveBytes.byteLength
        firstFileCount = built.fileCount
        firstChecksums = built.checksums
      }
    }

    const artifactsManifestPath = path.join(outDir, 'artifacts.json')
    const manifestDoc = {
      id: manifest.id,
      version: manifest.version,
      runtime: 'native',
      artifacts: Object.fromEntries(
        targets.map((target) => {
          const item = artifacts[target]!
          return [
            target,
            {
              file: item.file,
              sha256: item.sha256,
              size: item.size,
              ...(item.signature ? { signature: item.signature } : {}),
            },
          ]
        }),
      ),
    }
    writeFileSync(artifactsManifestPath, `${JSON.stringify(manifestDoc, null, 2)}\n`, 'utf8')

    return {
      archivePath: firstArchivePath,
      archiveBytes: firstArchiveBytes,
      fileCount: firstFileCount,
      checksums: firstChecksums,
      signed: Boolean(privPem),
      artifacts,
      artifactsManifestPath,
    }
  }

  const entries: ZipEntryInput[] = []
  let binaryCount = 0
  for (const rel of relFiles) {
    const abs = path.join(buildDir, ...rel.split('/'))
    const size = statSync(abs).size
    const isNativeBin = isNative && isNativeExecutablePath(manifest, rel)
    if (isNativeBin) {
      binaryCount++
    }
    const fileCap = isNativeBin ? MAX_NATIVE_BINARY_BYTES : MAX_ASSET_FILE_BYTES
    if (size > fileCap) {
      throw new Error(`File "${rel}" (${size} bytes) exceeds ${Math.round(fileCap / (1024 * 1024))} MiB limit`)
    }
    entries.push({
      name: rel,
      data: readFileSync(abs),
    })
  }

  const isFat = isNative && (Boolean(options.fat) || binaryCount > 1 || (manifest.native?.platforms?.length ?? 0) > 1)
  const built = buildArchiveFromEntries(entries, manifest, isFat, privPem)

  const defaultName = `${manifest.id}-${manifest.version}.slp`
  const targetBaseDir = options.outDir ? path.resolve(projectDir, options.outDir) : projectDir
  const archivePath = path.resolve(targetBaseDir, options.outFile ?? defaultName)
  mkdirSync(path.dirname(archivePath), { recursive: true })
  writeFileSync(archivePath, built.archiveBytes)

  return {
    archivePath,
    archiveBytes: built.archiveBytes.byteLength,
    fileCount: built.fileCount,
    checksums: built.checksums,
    signed: Boolean(privPem),
  }
}

/** Signs or re-signs an existing `.slp` archive with an Ed25519 private key. */
export function signPackage(options: SignOptions): SignResult {
  const archivePath = path.resolve(options.archivePath)
  const outPath = path.resolve(options.outPath ?? archivePath)
  const privPem = resolveKey(options.privateKeyPem, options.privateKeyFile, 'private key')

  const existing = readZip(readFileSync(archivePath)).filter(
    (entry) => entry.name !== 'checksums.txt' && entry.name !== 'signature',
  )
  const manifest = parseManifestFromEntries(existing)
  const { maxArchive } = resolveArchiveCaps(manifest, isFatEntries(manifest, existing))

  const checksums = computeChecksums(existing)
  const signatureText = signChecksums(checksums, privPem)
  const signedEntries: ZipEntryInput[] = [
    ...existing.map((e) => ({ name: e.name, data: e.data })),
    { name: 'checksums.txt', data: Buffer.from(checksums, 'utf8') },
    { name: 'signature', data: Buffer.from(signatureText, 'utf8') },
  ]
  const signedBytes = writeZip(signedEntries)
  if (signedBytes.byteLength > maxArchive) {
    throw new Error(
      `Signed archive (${signedBytes.byteLength} bytes) exceeds ${Math.round(maxArchive / (1024 * 1024))} MiB package cap`,
    )
  }
  mkdirSync(path.dirname(outPath), { recursive: true })
  writeFileSync(outPath, signedBytes)

  return {
    archivePath: outPath,
    checksums,
    signatureBase64: signatureText.trim(),
  }
}

/** Verifies `.slp` SHA-256 `checksums.txt` integrity and optional Ed25519 signature. */
export function verifyPackage(options: VerifyOptions): VerifyResult {
  const archivePath = path.resolve(options.archivePath)
  const rawArchive = readFileSync(archivePath)
  if (rawArchive.byteLength > MAX_NATIVE_FAT_PACKAGE_BYTES) {
    return {
      ok: false,
      errors: [`Archive (${rawArchive.byteLength} bytes) exceeds 150 MiB hard package cap`],
      fileCount: 0,
      signed: false,
    }
  }
  const entries = readZip(rawArchive)
  const errors: string[] = []
  const byName = new Map<string, Uint8Array>()
  for (const entry of entries) {
    if (byName.has(entry.name)) {
      errors.push(`Duplicate archive entry "${entry.name}"`)
      continue
    }
    byName.set(entry.name, entry.data)
  }

  const manifest = parseManifestFromEntries(entries)
  const isFat = isFatEntries(manifest, entries)
  const { maxArchive, maxUnpacked } = resolveArchiveCaps(manifest, isFat)
  if (rawArchive.byteLength > maxArchive) {
    errors.push(`Archive (${rawArchive.byteLength} bytes) exceeds ${Math.round(maxArchive / (1024 * 1024))} MiB package cap`)
  }

  if (!byName.has('plugin.json')) {
    errors.push('Archive is missing plugin.json')
  } else if (!manifest) {
    errors.push('Archive contains invalid plugin.json JSON')
  } else {
    const isNative = effectiveRuntime(manifest) === 'native'
    const cleanServer = manifest.server ? path.posix.normalize(manifest.server.replace(/^\.\//, '')) : ''
    if (!isNative && cleanServer && !byName.has(cleanServer)) {
      errors.push(`Declared server entry "${manifest.server}" is missing from archive`)
    }
    if (manifest.web) {
      const cleanWeb = path.posix.normalize(manifest.web.replace(/^\.\//, ''))
      if (!byName.has(cleanWeb)) {
        errors.push(`Declared web entry "${manifest.web}" is missing from archive`)
      }
    }
    let totalRaw = 0
    for (const entry of entries) {
      totalRaw += entry.data.byteLength
      const isBin = isNative && isNativeExecutablePath(manifest, entry.name)
      const cap = isBin ? MAX_NATIVE_BINARY_BYTES : MAX_ASSET_FILE_BYTES
      if (entry.data.byteLength > cap) {
        errors.push(`Entry "${entry.name}" (${entry.data.byteLength} bytes) exceeds ${Math.round(cap / (1024 * 1024))} MiB limit`)
      }
      if (!isNative && cleanServer && entry.name === cleanServer && entry.data.byteLength > MAX_SERVER_BUNDLE_BYTES) {
        errors.push(`Server bundle "${entry.name}" (${entry.data.byteLength} bytes) exceeds 5 MiB limit`)
      }
    }
    if (totalRaw > maxUnpacked) {
      errors.push(`Total unpacked size (${totalRaw} bytes) exceeds ${Math.round(maxUnpacked / (1024 * 1024))} MiB limit`)
    }
  }

  const checksumEntry = byName.get('checksums.txt')
  const signatureEntry = byName.get('signature')
  const signed = Boolean(signatureEntry)

  if (!checksumEntry) {
    errors.push('Archive is missing checksums.txt')
    return { ok: false, errors, fileCount: entries.length, signed }
  }

  const checksumsText = Buffer.from(checksumEntry).toString('utf8')
  const expectedMap = new Map<string, string>()
  for (const rawLine of checksumsText.split(/\r?\n/)) {
    if (!rawLine) continue
    const match = /^([0-9a-f]{64}) {2}(.+)$/.exec(rawLine)
    if (!match) {
      errors.push(`Malformed checksum line: ${rawLine}`)
      continue
    }
    if (expectedMap.has(match[2])) {
      errors.push(`Duplicate checksum line for "${match[2]}"`)
      continue
    }
    expectedMap.set(match[2], match[1])
  }

  for (const entry of entries) {
    if (entry.name === 'checksums.txt' || entry.name === 'signature') continue
    const expectedHex = expectedMap.get(entry.name)
    if (!expectedHex) {
      errors.push(`File "${entry.name}" is not listed in checksums.txt`)
      continue
    }
    const actualHex = createHash('sha256').update(entry.data).digest('hex')
    if (actualHex !== expectedHex) {
      errors.push(`Checksum mismatch for "${entry.name}"`)
    }
    expectedMap.delete(entry.name)
  }

  for (const missing of expectedMap.keys()) {
    errors.push(`Listed checksum entry "${missing}" is missing from archive`)
  }

  if (options.publicKeyPem || options.publicKeyFile) {
    if (!signatureEntry) {
      errors.push('Public key provided, but archive has no signature file')
    } else {
      try {
        const pubPem = resolveKey(options.publicKeyPem, options.publicKeyFile, 'public key')
        const pubKeyObj = createPublicKey(pubPem)
        if (pubKeyObj.asymmetricKeyType !== 'ed25519') {
          errors.push(`Expected Ed25519 public key, got ${String(pubKeyObj.asymmetricKeyType)}`)
        } else {
          const sigB64 = Buffer.from(signatureEntry).toString('utf8').trim()
          if (!/^[A-Za-z0-9+/]+={0,2}$/.test(sigB64)) {
            errors.push('Malformed base64 Ed25519 signature')
          } else {
            const sigBytes = Buffer.from(sigB64, 'base64')
            if (sigBytes.length !== 64 || !verify(null, Buffer.from(checksumEntry), pubKeyObj, sigBytes)) {
              errors.push('Ed25519 signature verification failed')
            }
          }
        }
      } catch (err) {
        errors.push(`Signature verification error: ${err instanceof Error ? err.message : String(err)}`)
      }
    }
  }

  return {
    ok: errors.length === 0,
    errors,
    fileCount: entries.length,
    signed,
  }
}
