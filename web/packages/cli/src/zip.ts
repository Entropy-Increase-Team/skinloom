import { deflateRawSync, inflateRawSync } from 'node:zlib'
import path from 'node:path'
import {
  effectiveRuntime,
  isNativeExecutablePath,
  MAX_NATIVE_BINARY_BYTES,
  MAX_NATIVE_UNPACKED_FAT_BYTES,
  MAX_NATIVE_UNPACKED_SINGLE_BYTES,
  type ManifestJson,
} from './manifest.js'

export interface ZipEntryInput {
  name: string
  data: Uint8Array
  mode?: number
}

export interface ZipEntryOutput {
  name: string
  data: Uint8Array
}

export interface ReadZipOptions {
  maxFileBytes?: number
  maxTotalBytes?: number
}

const crcTable = (() => {
  const table = new Uint32Array(256)
  for (let i = 0; i < 256; i++) {
    let c = i
    for (let k = 0; k < 8; k++) {
      c = (c & 1) !== 0 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
    }
    table[i] = c >>> 0
  }
  return table
})()

function crc32(buf: Uint8Array): number {
  let c = 0xffffffff
  for (let i = 0; i < buf.length; i++) {
    c = crcTable[(c ^ buf[i]) & 0xff] ^ (c >>> 8)
  }
  return (c ^ 0xffffffff) >>> 0
}

export const MAX_ZIP_ENTRIES = 4096
export const MAX_ZIP_FILE_BYTES = 10 * 1024 * 1024
export const MAX_ZIP_TOTAL_BYTES = 20 * 1024 * 1024

export function normalizeZipName(raw: string): string {
  if (
    !raw ||
    /[\x00-\x1f\x7f]/.test(raw) ||
    raw.includes('\\') ||
    raw.startsWith('/') ||
    raw.endsWith('/') ||
    raw.split('/').some((seg) => seg === '' || seg === '.' || seg === '..')
  ) {
    throw new Error(`unsafe archive entry path: "${raw}"`)
  }
  const clean = path.posix.normalize(raw)
  if (!clean || clean === '.' || clean.split('/').some((seg) => seg === '' || seg === '.' || seg === '..')) {
    throw new Error(`unsafe archive entry path: "${raw}"`)
  }
  return clean
}

/** Writes a deterministic deflated ZIP archive sorted by normalized entry path. */
export function writeZip(entries: ZipEntryInput[]): Uint8Array {
  if (entries.length > MAX_ZIP_ENTRIES || entries.length > 0xffff) {
    throw new Error(`archive entry count ${entries.length} exceeds limit`)
  }
  const seen = new Set<string>()
  const sorted = [...entries]
    .map((e) => {
      const name = normalizeZipName(e.name)
      const lower = name.toLowerCase()
      if (seen.has(lower)) {
        throw new Error(`duplicate archive entry path: "${name}"`)
      }
      seen.add(lower)
      return { name, data: e.data, mode: e.mode ?? 0o100644 }
    })
    .sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0))

  const localParts: Buffer[] = []
  const centralParts: Buffer[] = []
  let offset = 0

  for (const entry of sorted) {
    const nameBuf = Buffer.from(entry.name, 'utf8')
    const raw = Buffer.from(entry.data)
    const deflated = deflateRawSync(raw, { level: 6 })
    const useDeflate = deflated.length < raw.length
    const method = useDeflate ? 8 : 0
    const compressed = useDeflate ? deflated : raw
    if (offset > 0xffffffff || compressed.length > 0xffffffff || raw.length > 0xffffffff || nameBuf.length > 0xffff) {
      throw new Error(`archive entry "${entry.name}" exceeds ZIP32 limits`)
    }
    const sum = crc32(raw)

    const local = Buffer.alloc(30)
    local.writeUInt32LE(0x04034b50, 0)
    local.writeUInt16LE(20, 4) // version needed
    local.writeUInt16LE(0x0800, 6) // UTF-8 flag
    local.writeUInt16LE(method, 8)
    local.writeUInt16LE(0, 10) // time
    local.writeUInt16LE(0x5421, 12) // deterministic date
    local.writeUInt32LE(sum, 14)
    local.writeUInt32LE(compressed.length, 18)
    local.writeUInt32LE(raw.length, 22)
    local.writeUInt16LE(nameBuf.length, 26)
    local.writeUInt16LE(0, 28)

    localParts.push(local, nameBuf, compressed)

    const central = Buffer.alloc(46)
    central.writeUInt32LE(0x02014b50, 0)
    central.writeUInt16LE((3 << 8) | 20, 4) // Unix creator + version 2.0
    central.writeUInt16LE(20, 6)
    central.writeUInt16LE(0x0800, 8)
    central.writeUInt16LE(method, 10)
    central.writeUInt16LE(0, 12)
    central.writeUInt16LE(0x5421, 14)
    central.writeUInt32LE(sum, 16)
    central.writeUInt32LE(compressed.length, 20)
    central.writeUInt32LE(raw.length, 24)
    central.writeUInt16LE(nameBuf.length, 28)
    central.writeUInt16LE(0, 30)
    central.writeUInt16LE(0, 32)
    central.writeUInt16LE(0, 34)
    central.writeUInt16LE(0, 36)
    central.writeUInt32LE(((entry.mode & 0xffff) << 16) >>> 0, 38)
    central.writeUInt32LE(offset, 42)

    centralParts.push(central, nameBuf)
    offset += local.length + nameBuf.length + compressed.length
  }

  const centralSize = centralParts.reduce((s, b) => s + b.length, 0)
  if (offset > 0xffffffff || centralSize > 0xffffffff || offset + centralSize > 0xffffffff) {
    throw new Error('archive exceeds ZIP32 size limits')
  }
  const eocd = Buffer.alloc(22)
  eocd.writeUInt32LE(0x06054b50, 0)
  eocd.writeUInt16LE(0, 4)
  eocd.writeUInt16LE(0, 6)
  eocd.writeUInt16LE(sorted.length, 8)
  eocd.writeUInt16LE(sorted.length, 10)
  eocd.writeUInt32LE(centralSize, 12)
  eocd.writeUInt32LE(offset, 16)
  eocd.writeUInt16LE(0, 20)

  return new Uint8Array(Buffer.concat([...localParts, ...centralParts, eocd]))
}

interface ParsedCdEntry {
  cleanName: string
  method: number
  expectedCrc: number
  compSize: number
  uncompSize: number
  localOff: number
}

function extractEntryBytes(buf: Buffer, cdOffset: number, rec: ParsedCdEntry, maxBytes: number): Buffer {
  if (rec.uncompSize > maxBytes) {
    throw new Error(`archive entry "${rec.cleanName}" exceeds ${maxBytes} byte limit`)
  }
  if (rec.localOff + 30 > cdOffset || buf.readUInt32LE(rec.localOff) !== 0x04034b50) {
    throw new Error(`corrupt local file header for ${rec.cleanName}`)
  }
  const lFlags = buf.readUInt16LE(rec.localOff + 6)
  if ((lFlags & 0x0001) !== 0) {
    throw new Error(`encrypted archive entry "${rec.cleanName}" is not supported`)
  }
  const lMethod = buf.readUInt16LE(rec.localOff + 8)
  if (lMethod !== rec.method) {
    throw new Error(`local/central compression method mismatch for ${rec.cleanName}`)
  }
  const lNameLen = buf.readUInt16LE(rec.localOff + 26)
  const lExtraLen = buf.readUInt16LE(rec.localOff + 28)
  const dataStart = rec.localOff + 30 + lNameLen + lExtraLen
  if (rec.localOff + 30 + lNameLen > cdOffset || dataStart + rec.compSize > cdOffset) {
    throw new Error(`corrupt data bounds for ${rec.cleanName}`)
  }
  const lRawName = buf.subarray(rec.localOff + 30, rec.localOff + 30 + lNameLen).toString('utf8')
  if (normalizeZipName(lRawName) !== rec.cleanName) {
    throw new Error(`local/central filename mismatch for ${rec.cleanName}`)
  }
  const slice = buf.subarray(dataStart, dataStart + rec.compSize)
  let data: Buffer
  if (rec.method === 0) {
    if (rec.compSize !== rec.uncompSize || slice.length !== rec.uncompSize) {
      throw new Error(`stored size mismatch for ${rec.cleanName}`)
    }
    data = Buffer.from(slice)
  } else if (rec.method === 8) {
    data = inflateRawSync(slice, { maxOutputLength: maxBytes })
    if (data.length !== rec.uncompSize) {
      throw new Error(`uncompressed size mismatch for ${rec.cleanName}`)
    }
  } else {
    throw new Error(`unsupported zip compression method ${rec.method} for ${rec.cleanName}`)
  }
  if (crc32(data) !== rec.expectedCrc) {
    throw new Error(`CRC mismatch reading ${rec.cleanName}`)
  }
  return data
}

/** Reads entries from a standard ZIP archive with Zip-Slip, bounds, and CRC validation. */
export function readZip(archiveBytes: Uint8Array, options?: ReadZipOptions): ZipEntryOutput[] {
  const buf = Buffer.from(archiveBytes)
  if (buf.length < 22) {
    throw new Error('invalid .slp zip archive: truncated buffer')
  }
  let eocdOffset = -1
  for (let i = buf.length - 22; i >= Math.max(0, buf.length - 65558); i--) {
    if (buf.readUInt32LE(i) === 0x06054b50) {
      const commentLen = buf.readUInt16LE(i + 20)
      if (i + 22 + commentLen === buf.length) {
        eocdOffset = i
        break
      }
    }
  }
  if (eocdOffset < 0) {
    throw new Error('invalid .slp zip archive: EOCD record not found')
  }
  const diskNum = buf.readUInt16LE(eocdOffset + 4)
  const cdDisk = buf.readUInt16LE(eocdOffset + 6)
  const diskEntries = buf.readUInt16LE(eocdOffset + 8)
  const totalEntries = buf.readUInt16LE(eocdOffset + 10)
  const cdSize = buf.readUInt32LE(eocdOffset + 12)
  const cdOffset = buf.readUInt32LE(eocdOffset + 16)
  if (diskNum !== 0 || cdDisk !== 0 || diskEntries !== totalEntries) {
    throw new Error('multi-disk .slp zip archives are not supported')
  }
  if (totalEntries > MAX_ZIP_ENTRIES) {
    throw new Error(`archive has ${totalEntries} entries (limit ${MAX_ZIP_ENTRIES})`)
  }
  if (cdOffset > eocdOffset || cdOffset + cdSize > eocdOffset) {
    throw new Error('corrupt .slp central directory offset')
  }

  const records: ParsedCdEntry[] = []
  const seen = new Set<string>()
  let ptr = cdOffset
  for (let i = 0; i < totalEntries; i++) {
    if (ptr + 46 > eocdOffset || buf.readUInt32LE(ptr) !== 0x02014b50) {
      throw new Error('corrupt .slp central directory header')
    }
    const versionMadeBy = buf.readUInt16LE(ptr + 4)
    const gpFlag = buf.readUInt16LE(ptr + 8)
    const method = buf.readUInt16LE(ptr + 10)
    const expectedCrc = buf.readUInt32LE(ptr + 16)
    const compSize = buf.readUInt32LE(ptr + 20)
    const uncompSize = buf.readUInt32LE(ptr + 24)
    const nameLen = buf.readUInt16LE(ptr + 28)
    const extraLen = buf.readUInt16LE(ptr + 30)
    const commentLen = buf.readUInt16LE(ptr + 32)
    const extAttr = buf.readUInt32LE(ptr + 38)
    const localOff = buf.readUInt32LE(ptr + 42)
    if (ptr + 46 + nameLen + extraLen + commentLen > eocdOffset) {
      throw new Error('corrupt .slp central directory record length')
    }
    const rawName = buf.subarray(ptr + 46, ptr + 46 + nameLen).toString('utf8')
    ptr += 46 + nameLen + extraLen + commentLen

    if ((gpFlag & 0x0001) !== 0) {
      throw new Error(`encrypted archive entry "${rawName}" is not supported`)
    }
    if ((versionMadeBy >> 8) === 3) {
      const unixMode = (extAttr >>> 16) & 0o170000
      if (unixMode === 0o120000) {
        throw new Error(`symlink archive entry "${rawName}" is not allowed`)
      }
    }
    if (rawName.endsWith('/')) {
      normalizeZipName(rawName.slice(0, -1))
      continue
    }
    const cleanName = normalizeZipName(rawName)
    const lower = cleanName.toLowerCase()
    if (seen.has(lower)) {
      throw new Error(`duplicate archive entry "${cleanName}"`)
    }
    seen.add(lower)
    records.push({ cleanName, method, expectedCrc, compSize, uncompSize, localOff })
  }

  let manifest: ManifestJson | undefined
  const manifestRec = records.find((r) => r.cleanName === 'plugin.json')
  if (manifestRec) {
    const mBytes = extractEntryBytes(buf, cdOffset, manifestRec, options?.maxFileBytes ?? MAX_ZIP_FILE_BYTES)
    try {
      manifest = JSON.parse(mBytes.toString('utf8')) as ManifestJson
    } catch {
      manifest = undefined
    }
  }

  const isNative = Boolean(manifest && effectiveRuntime(manifest) === 'native')
  let nativeBinCount = 0
  if (isNative && manifest) {
    for (const r of records) {
      if (isNativeExecutablePath(manifest, r.cleanName)) {
        nativeBinCount++
      }
    }
  }
  const isFatNative = isNative && (nativeBinCount > 1 || (manifest?.native?.platforms?.length ?? 0) > 1)
  const maxTotalBytes =
    options?.maxTotalBytes ??
    (isNative
      ? isFatNative
        ? MAX_NATIVE_UNPACKED_FAT_BYTES
        : MAX_NATIVE_UNPACKED_SINGLE_BYTES
      : MAX_ZIP_TOTAL_BYTES)

  const out: ZipEntryOutput[] = []
  let totalUnpacked = 0
  for (const rec of records) {
    const fileCap =
      options?.maxFileBytes ??
      (isNative && manifest && isNativeExecutablePath(manifest, rec.cleanName)
        ? MAX_NATIVE_BINARY_BYTES
        : MAX_ZIP_FILE_BYTES)
    if (rec.uncompSize > fileCap) {
      throw new Error(`archive entry "${rec.cleanName}" exceeds ${fileCap} byte limit`)
    }
    const remainingTotal = maxTotalBytes - totalUnpacked
    if (rec.uncompSize > remainingTotal) {
      throw new Error(`unpacked archive size exceeds ${maxTotalBytes} byte cap`)
    }
    const data = extractEntryBytes(buf, cdOffset, rec, Math.min(fileCap, remainingTotal))
    totalUnpacked += data.length
    if (totalUnpacked > maxTotalBytes) {
      throw new Error(`unpacked archive size exceeds ${maxTotalBytes} byte cap`)
    }
    out.push({ name: rec.cleanName, data: new Uint8Array(data) })
  }
  return out
}
