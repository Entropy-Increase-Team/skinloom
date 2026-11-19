import { ApiError, apiFetch } from './fetcher'
import type {
  AdminPluginView,
  CreateUploadInputPurpose,
  CreateUploadResult,
  PackageInspectResult,
  UploadView,
} from './gen/model'

export interface TextureUpload {
  file: Blob
  name: string
  type: 'steve' | 'alex' | 'cape'
  public: boolean
  form?: Record<string, unknown>
}

export interface SetupUploadProgress {
  loaded: number
  total: number
}

const DEFAULT_CHUNK_BYTES = 8 * 1024 * 1024
const CHUNK_MAX_ATTEMPTS = 3

function isTransientChunkError(err: unknown): boolean {
  if (err instanceof DOMException && err.name === 'AbortError') return false
  if (err instanceof ApiError) return err.status === 0 || err.status >= 500
  return true
}

/**
 * Uploads a skin or cape (POST /api/v1/textures). Written by hand: the generated
 * uploadTexture sends the text fields as Blob parts, which browsers transmit as file
 * parts, and the server reads them only as plain form values.
 */
export async function uploadTextureFile(input: TextureUpload): Promise<UploadView> {
  const form = new FormData()
  // Declare the part as PNG even when the browser does not know the file's type.
  const file = input.file.type === 'image/png' ? input.file : new Blob([input.file], { type: 'image/png' })
  form.append('file', file, 'texture.png')
  form.append('name', input.name)
  form.append('type', input.type)
  form.append('public', String(input.public))
  if (input.form && Object.keys(input.form).length > 0) {
    form.append('form', JSON.stringify(input.form))
  }
  const res = await apiFetch<{ data: UploadView }>('/api/v1/textures', { method: 'POST', body: form })
  return res.data
}

/**
 * Uploads a backup or Blessing Skin textures archive in fixed binary chunks
 * (POST /api/v1/setup/uploads + PUT /api/v1/setup/uploads/{id}/chunks).
 */
export async function uploadSetupArchive(
  purpose: CreateUploadInputPurpose,
  file: File,
  onProgress?: (p: SetupUploadProgress) => void,
  signal?: AbortSignal,
): Promise<{ uploadId: string }> {
  const created = await apiFetch<{ data: CreateUploadResult }>('/api/v1/setup/uploads', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ purpose, filename: file.name, totalSize: file.size }),
    signal,
  })
  const { uploadId, chunkSize: rawChunkSize } = created.data
  const chunkSize = rawChunkSize > 0 ? rawChunkSize : DEFAULT_CHUNK_BYTES

  onProgress?.({ loaded: 0, total: file.size })
  for (let offset = 0; offset < file.size; offset += chunkSize) {
    if (signal?.aborted) throw signal.reason ?? new DOMException('Aborted', 'AbortError')
    const end = Math.min(offset + chunkSize, file.size)
    const slice = file.slice(offset, end)
    let lastErr: unknown
    for (let attempt = 1; attempt <= CHUNK_MAX_ATTEMPTS; attempt++) {
      try {
        await apiFetch(`/api/v1/setup/uploads/${encodeURIComponent(uploadId)}/chunks?offset=${offset}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/octet-stream' },
          body: slice,
          signal,
        })
        lastErr = undefined
        break
      } catch (err) {
        lastErr = err
        if (attempt === CHUNK_MAX_ATTEMPTS || !isTransientChunkError(err) || signal?.aborted) {
          throw err
        }
      }
    }
    if (lastErr !== undefined) throw lastErr
    onProgress?.({ loaded: end, total: file.size })
  }

  return { uploadId }
}

export interface PluginInstallUpload {
  file: File | Blob
  grantedPermissions: string[]
  allowUnsigned?: boolean
  disabled?: boolean
}

/**
 * Inspects a `.slp` package archive without installing it (POST /api/v1/admin/plugins/inspect).
 */
export async function inspectPluginFile(file: File | Blob, signal?: AbortSignal): Promise<PackageInspectResult> {
  const form = new FormData()
  form.append('file', file, 'name' in file && file.name ? file.name : 'plugin.slp')
  const res = await apiFetch<{ data: PackageInspectResult }>('/api/v1/admin/plugins/inspect', {
    method: 'POST',
    body: form,
    signal,
  })
  return res.data
}

/**
 * Installs or upgrades a `.slp` plugin/theme archive (POST /api/v1/admin/plugins/install).
 */
export async function installPluginFile(input: PluginInstallUpload, signal?: AbortSignal): Promise<AdminPluginView> {
  const form = new FormData()
  form.append('file', input.file, 'name' in input.file && input.file.name ? input.file.name : 'plugin.slp')
  form.append('grantedPermissions', JSON.stringify(input.grantedPermissions))
  if (input.allowUnsigned) form.append('allowUnsigned', 'true')
  if (input.disabled) form.append('disabled', 'true')
  const res = await apiFetch<{ data: AdminPluginView }>('/api/v1/admin/plugins/install', {
    method: 'POST',
    body: form,
    signal,
  })
  return res.data
}
