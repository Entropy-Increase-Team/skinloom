import { apiFetch } from './fetcher'
import type { UploadView } from './gen/api'

export interface TextureUpload {
  file: Blob
  name: string
  type: 'steve' | 'alex' | 'cape'
  public: boolean
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
  const res = await apiFetch<{ data: UploadView }>('/api/v1/textures', { method: 'POST', body: form })
  return res.data
}
