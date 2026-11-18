'use client'

import {
  ApiError,
  type DBConfigInput,
  executeSetupRestore,
  getSetupJob,
  type InspectRestoreResult,
  inspectSetupRestore,
  type SetupJobView,
  type SetupState,
  type StartJobResult,
  type StorageConfigInput,
  uploadSetupArchive,
} from '@skinloom/api-client'
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Button,
  Checkbox,
  Dropzone,
  Progress,
  ProgressLabel,
  Separator,
  Tabs,
  TabsList,
  TabsTrigger,
} from '@skinloom/ui'
import { FileArchive } from 'lucide-react'
import { useLocale, useTranslations } from 'next-intl'
import { useEffect, useRef, useState } from 'react'

import { Field, FormError } from '@/components/auth/field'

import { formatSetupBytes, setupErrorMessage } from './types'

export function RestoreStep({
  state,
  db,
  storage,
  trustedProxies,
  initialJobId,
  onBack,
  onCompleted,
}: {
  state: SetupState
  db: DBConfigInput
  storage: StorageConfigInput
  trustedProxies: string[]
  initialJobId?: string
  onBack: () => void
  onCompleted: (redirectTo: string) => void
}) {
  const t = useTranslations('setup.restore')
  const locale = useLocale()
  const [sourceTab, setSourceTab] = useState<'upload' | 'path'>('upload')
  const [uploadId, setUploadId] = useState('')
  const [uploadName, setUploadName] = useState('')
  const [uploadProgress, setUploadProgress] = useState<{ loaded: number; total: number } | null>(null)
  const [uploading, setUploading] = useState(false)
  const abortRef = useRef<AbortController | null>(null)

  const [path, setPath] = useState('')
  const [inspecting, setInspecting] = useState(false)
  const [inspectRes, setInspectRes] = useState<InspectRestoreResult | null>(null)
  const [confirmed, setConfirmed] = useState(false)
  const [starting, setStarting] = useState(false)
  const [job, setJob] = useState<SetupJobView | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!initialJobId) return
    let active = true
    void (async () => {
      try {
        const res = await getSetupJob(initialJobId)
        if (active) setJob(res.data as SetupJobView)
      } catch {
        // Ignore stale job id
      }
    })()
    return () => {
      active = false
    }
  }, [initialJobId])

  useEffect(() => {
    if (!job || job.status !== 'running') return
    const timer = setInterval(async () => {
      try {
        const res = await getSetupJob(job.id)
        const next = res.data as SetupJobView
        setJob(next)
        if (next.status === 'succeeded') {
          clearInterval(timer)
          onCompleted(next.redirectTo || '/auth/login')
        }
      } catch (err) {
        if (err instanceof ApiError && err.status === 409) {
          // Server already completed restore and atomically switched to Running Mode.
          clearInterval(timer)
          setJob((prev) => (prev ? { ...prev, status: 'succeeded', percent: 100, redirectTo: '/auth/login' } : prev))
          onCompleted('/auth/login')
        }
      }
    }, 500)
    return () => clearInterval(timer)
  }, [job, onCompleted])

  async function handleFile(file: File) {
    setError(null)
    setInspectRes(null)
    setUploadId('')
    if (file.size > state.maxUploadBytes) {
      setError(t('tooLarge', { max: formatSetupBytes(state.maxUploadBytes, locale) }))
      return
    }
    const ctl = new AbortController()
    abortRef.current = ctl
    setUploading(true)
    setUploadName(file.name)
    try {
      const res = await uploadSetupArchive('backup', file, setUploadProgress, ctl.signal)
      setUploadId(res.uploadId)
    } catch (err) {
      if (!(err instanceof DOMException && err.name === 'AbortError')) {
        setError(setupErrorMessage(err, t('uploadFailed')))
      }
    } finally {
      setUploading(false)
      abortRef.current = null
    }
  }

  async function runInspect() {
    setInspecting(true)
    setError(null)
    try {
      const res = await inspectSetupRestore(
        sourceTab === 'upload' ? { uploadId } : { path: path.trim() },
      )
      setInspectRes(res.data as InspectRestoreResult)
    } catch (err) {
      setError(setupErrorMessage(err, t('inspectFailed')))
    } finally {
      setInspecting(false)
    }
  }

  async function startRestore() {
    setStarting(true)
    setError(null)
    try {
      const res = await executeSetupRestore({
        ...(sourceTab === 'upload' ? { uploadId } : { path: path.trim() }),
        database: state.envLocked.database ? { useEnv: true } : db,
        storage: state.envLocked.storage ? { useEnv: true } : storage,
        trustedProxies,
      })
      const started = res.data as StartJobResult
      setJob({
        id: started.jobId,
        kind: 'restore',
        status: 'running',
        stage: 'restore',
        done: 0,
        total: 0,
        percent: 0,
        hasReport: false,
        updatedAt: new Date().toISOString(),
      })
    } catch (err) {
      setError(setupErrorMessage(err, t('executeFailed')))
      setStarting(false)
    }
  }

  const running = Boolean(job && job.status === 'running')

  if (job) {
    return (
      <div className="flex flex-col gap-5">
        <div className="flex flex-col gap-1">
          <h2 className="text-lg font-medium">{t('runningTitle')}</h2>
          <p className="text-sm text-muted-foreground">{job.message || job.stage}</p>
        </div>

        <Progress value={job.percent}>
          <ProgressLabel>{job.stage}</ProgressLabel>
          <span className="ml-auto text-sm text-muted-foreground tabular-nums">{job.percent}%</span>
        </Progress>

        {job.status === 'failed' && (
          <Alert variant="destructive">
            <AlertTitle>{t('failedTitle')}</AlertTitle>
            <AlertDescription>{job.error}</AlertDescription>
          </Alert>
        )}

        {job.status === 'succeeded' && (
          <Alert>
            <AlertTitle>{t('doneTitle')}</AlertTitle>
            <AlertDescription className="flex flex-col gap-3">
              <span>{t('doneDesc')}</span>
              <div>
                <Button onClick={() => onCompleted(job.redirectTo || '/auth/login')}>
                  {t('goLogin')}
                </Button>
              </div>
            </AlertDescription>
          </Alert>
        )}

        {job.status === 'failed' && (
          <div className="flex justify-between">
            <Button variant="outline" onClick={() => setJob(null)}>
              {t('retry')}
            </Button>
          </div>
        )}
      </div>
    )
  }

  const canInspect = sourceTab === 'upload' ? Boolean(uploadId) && !uploading : Boolean(path.trim())

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h2 className="text-lg font-medium">{t('title')}</h2>
        <p className="text-sm text-muted-foreground">{t('intro')}</p>
      </div>

      <FormError message={error} />

      <Tabs
        value={sourceTab}
        onValueChange={(v) => {
          setSourceTab(v as 'upload' | 'path')
          setInspectRes(null)
        }}
      >
        <TabsList className="h-auto! w-full flex-wrap justify-start gap-1 sm:w-fit">
          <TabsTrigger value="upload" className="py-1">{t('tabs.upload')}</TabsTrigger>
          <TabsTrigger value="path" className="py-1">{t('tabs.path')}</TabsTrigger>
        </TabsList>
      </Tabs>

      {sourceTab === 'upload' ? (
        <div className="flex flex-col gap-3">
          <Dropzone
            accept=".tar.gz,.tgz,.zip"
            disabled={uploading}
            hint={t('uploadHint', { max: formatSetupBytes(state.maxUploadBytes, locale) })}
            onFile={(f) => void handleFile(f)}
          >
            {uploadId || uploading ? (
              <div className="flex flex-col items-center gap-2">
                <FileArchive className="size-8 text-primary" aria-hidden />
                <span className="text-sm font-medium">{uploadName}</span>
                {uploadId && <span className="text-xs text-muted-foreground">{t('uploadedReady')}</span>}
              </div>
            ) : undefined}
          </Dropzone>

          {uploading && uploadProgress && (
            <div className="flex flex-col gap-2">
              <Progress value={uploadProgress.total > 0 ? Math.round((uploadProgress.loaded / uploadProgress.total) * 100) : 0}>
                <ProgressLabel>{t('uploading')}</ProgressLabel>
                <span className="ml-auto text-sm text-muted-foreground tabular-nums">
                  {formatSetupBytes(uploadProgress.loaded, locale)} / {formatSetupBytes(uploadProgress.total, locale)}
                </span>
              </Progress>
              <Button variant="outline" size="xs" className="self-end" onClick={() => abortRef.current?.abort()}>
                {t('cancelUpload')}
              </Button>
            </div>
          )}
        </div>
      ) : (
        <Field
          id="restore-local-path"
          label={t('pathLabel')}
          hint={t('pathHint')}
          placeholder="/var/backups/skinloom-backup.tar.gz"
          value={path}
          onChange={(e) => {
            setPath(e.target.value)
            setInspectRes(null)
          }}
        />
      )}

      <div>
        <Button variant="outline" disabled={!canInspect} loading={inspecting} onClick={() => void runInspect()}>
          {t('inspect')}
        </Button>
      </div>

      {inspectRes && (
        <div className="flex flex-col gap-4 rounded-xl border bg-card p-5">
          <h3 className="text-sm font-medium">{t('manifestTitle')}</h3>
          <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-sm sm:grid-cols-3">
            <div>
              <dt className="text-xs text-muted-foreground">{t('manifest.database')}</dt>
              <dd className="font-mono text-xs">{inspectRes.manifest.database}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{t('manifest.migration')}</dt>
              <dd className="font-mono text-xs">{inspectRes.manifest.migration}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{t('manifest.textures')}</dt>
              <dd className="font-medium tabular-nums">{inspectRes.manifest.textures}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{t('manifest.textureBytes')}</dt>
              <dd className="font-medium tabular-nums">{formatSetupBytes(inspectRes.manifest.textureBytes, locale)}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{t('manifest.created')}</dt>
              <dd className="text-xs tabular-nums">
                {inspectRes.manifest.createdAt
                  ? new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(
                      new Date(inspectRes.manifest.createdAt),
                    )
                  : '—'}
              </dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">{t('manifest.version')}</dt>
              <dd className="font-mono text-xs">{inspectRes.manifest.version}</dd>
            </div>
          </dl>

          {inspectRes.warnings.length > 0 && (
            <Alert>
              <AlertDescription className="flex flex-col gap-1 text-xs">
                {inspectRes.warnings.map((w, i) => (
                  <span key={i}>{w}</span>
                ))}
              </AlertDescription>
            </Alert>
          )}

          <label className="flex cursor-pointer items-start gap-2.5 text-sm">
            <Checkbox
              className="mt-0.5"
              checked={confirmed}
              onCheckedChange={(c) => setConfirmed(Boolean(c))}
            />
            <span>{t('confirmRestore')}</span>
          </label>
        </div>
      )}

      <Separator />

      <div className="flex flex-wrap items-center justify-between gap-3">
        <Button variant="outline" disabled={running || starting} onClick={onBack}>
          {t('back')}
        </Button>
        <Button disabled={!inspectRes || !confirmed} loading={starting} onClick={() => void startRestore()}>
          {t('startRestore')}
        </Button>
      </div>
    </div>
  )
}
