'use client'

import {
  ApiError,
  type BSSourceInput,
  type BSSourceInputDriver,
  type BSSourceInputSourceType,
  type BSSourceInputTextureSource,
  type DBConfigInput,
  type EnvInfo,
  getSetupJob,
  parseSetupBSEnv,
  type SetupJobView,
  type SetupState,
  type StartJobResult,
  startSetupBSDryRun,
  startSetupBSImport,
  type StorageConfigInput,
  type TestBSSourceResult,
  testSetupBSSource,
  uploadSetupArchive,
} from '@skinloom/api-client'
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
  Button,
  Checkbox,
  CopyField,
  Dropzone,
  Field as UiField,
  FieldLabel,
  Progress,
  ProgressLabel,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Separator,
  Tabs,
  TabsList,
  TabsTrigger,
} from '@skinloom/ui'
import { FileArchive, FileText } from 'lucide-react'
import { useLocale, useTranslations } from 'next-intl'
import { useEffect, useRef, useState } from 'react'

import { Field, FormError } from '@/components/auth/field'

import { BSReportView } from './bs-report-view'
import { formatSetupBytes, setupErrorMessage } from './types'

const PWD_METHODS = ['BCRYPT', 'MD5', 'SALTED2MD5', 'SHA256', 'SALTED2SHA256', 'SHA512', 'SALTED2SHA512', 'PHP_PASSWORD_HASH', 'ARGON2I', 'ARGON2ID']

export function BSMigrateStep({
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
  const t = useTranslations('setup.bs')
  const locale = useLocale()

  const [sourceType, setSourceType] = useState<'db' | 'bridge'>('db')
  const [envPath, setEnvPath] = useState('')
  const [envInfo, setEnvInfo] = useState<EnvInfo | null>(null)
  const [envId, setEnvId] = useState('')
  const [parsingEnv, setParsingEnv] = useState(false)

  const [driver, setDriver] = useState<BSSourceInputDriver>('mysql')
  const [rawDsnMode, setRawDsnMode] = useState(false)
  const [rawDsn, setRawDsn] = useState('')
  const [host, setHost] = useState('127.0.0.1')
  const [port, setPort] = useState(3306)
  const [database, setDatabase] = useState('blessing')
  const [username, setUsername] = useState('root')
  const [password, setPassword] = useState('')
  const [prefix, setPrefix] = useState('')
  const [passwordMethod, setPasswordMethod] = useState('BCRYPT')
  const [salt, setSalt] = useState('')
  const [timezone, setTimezone] = useState('Asia/Shanghai')

  const [textureSource, setTextureSource] = useState<'dir' | 'upload' | 'none'>('dir')
  const [texturesDir, setTexturesDir] = useState('')
  const [uploadId, setUploadId] = useState('')
  const [uploadName, setUploadName] = useState('')
  const [uploading, setUploading] = useState(false)
  const [uploadProgress, setUploadProgress] = useState<{ loaded: number; total: number } | null>(null)
  const uploadAbortRef = useRef<AbortController | null>(null)

  const [bridgeUrl, setBridgeUrl] = useState('')
  const [bridgeToken, setBridgeToken] = useState('')
  const [allowInsecureLocal, setAllowInsecureLocal] = useState(false)

  const [testing, setTesting] = useState(false)
  const [sourceTest, setSourceTest] = useState<TestBSSourceResult | null>(null)

  const [siteUrl, setSiteUrl] = useState(() =>
    typeof window !== 'undefined' ? state.suggestedSiteUrl || window.location.origin : state.suggestedSiteUrl || '',
  )
  const [fallbackEmail, setFallbackEmail] = useState('')
  const [fallbackNickname, setFallbackNickname] = useState('')
  const [fallbackPassword, setFallbackPassword] = useState('')

  const [dryJob, setDryJob] = useState<SetupJobView | null>(null)
  const [importJob, setImportJob] = useState<SetupJobView | null>(null)
  const [startingDry, setStartingDry] = useState(false)
  const [startingImport, setStartingImport] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!initialJobId) return
    let active = true
    void (async () => {
      try {
        const res = await getSetupJob(initialJobId)
        const j = res.data as SetupJobView
        if (!active) return
        if (j.kind === 'bs_dry_run') setDryJob(j)
        else if (j.kind === 'bs_import') setImportJob(j)
      } catch {
        // Ignore stale job
      }
    })()
    return () => {
      active = false
    }
  }, [initialJobId])

  const activePollingJob =
    importJob && importJob.status === 'running'
      ? importJob
      : dryJob && dryJob.status === 'running'
        ? dryJob
        : null

  useEffect(() => {
    if (!activePollingJob) return
    const kind = activePollingJob.kind
    const id = activePollingJob.id
    const timer = setInterval(async () => {
      try {
        const res = await getSetupJob(id)
        const next = res.data as SetupJobView
        if (kind === 'bs_dry_run') {
          setDryJob(next)
          setStartingDry(false)
        } else {
          setImportJob(next)
          setStartingImport(false)
        }
      } catch (err) {
        if (err instanceof ApiError && err.status === 409 && kind === 'bs_import') {
          // Server completed migration and transitioned to Running Mode.
          clearInterval(timer)
          setStartingImport(false)
          setImportJob((prev) =>
            prev ? { ...prev, status: 'succeeded', percent: 100, redirectTo: '/auth/login' } : prev,
          )
        }
      }
    }, 500)
    return () => clearInterval(timer)
  }, [activePollingJob])

  function applyParsedEnv(env: EnvInfo, parsedEnvId?: string) {
    setEnvInfo(env)
    setEnvId(parsedEnvId ?? '')
    setSourceTest(null)
    setDryJob(null)
    if (env.driver === 'mysql' || env.driver === 'postgres' || env.driver === 'sqlite') {
      setDriver(env.driver)
    }
    if (env.host) setHost(env.host)
    if (env.port) setPort(env.port)
    if (env.database) setDatabase(env.database)
    if (env.username) setUsername(env.username)
    setPrefix(env.prefix ?? '')
    setPasswordMethod(env.passwordMethod || 'BCRYPT')
    setPassword('')
    setSalt('')
    if (env.texturesDir) {
      setTexturesDir(env.texturesDir)
      setTextureSource('dir')
    }
  }

  async function handleEnvFile(file: File) {
    setParsingEnv(true)
    setError(null)
    try {
      const content = await file.text()
      const res = await parseSetupBSEnv({ content })
      const data = res.data as { env: EnvInfo; envId?: string }
      applyParsedEnv(data.env, data.envId)
    } catch (err) {
      setError(setupErrorMessage(err, t('envParseFailed')))
    } finally {
      setParsingEnv(false)
    }
  }

  async function handleEnvPathParse() {
    if (!envPath.trim()) return
    setParsingEnv(true)
    setError(null)
    try {
      const res = await parseSetupBSEnv({ path: envPath.trim() })
      const data = res.data as { env: EnvInfo; envId?: string }
      applyParsedEnv(data.env, data.envId)
    } catch (err) {
      setError(setupErrorMessage(err, t('envParseFailed')))
    } finally {
      setParsingEnv(false)
    }
  }

  async function handleTextureArchive(file: File) {
    setError(null)
    setSourceTest(null)
    setDryJob(null)
    setUploadId('')
    if (file.size > state.maxUploadBytes) {
      setError(t('texturesTooLarge', { max: formatSetupBytes(state.maxUploadBytes, locale) }))
      return
    }
    const ctl = new AbortController()
    uploadAbortRef.current = ctl
    setUploading(true)
    setUploadName(file.name)
    try {
      const res = await uploadSetupArchive('bs_textures', file, setUploadProgress, ctl.signal)
      setUploadId(res.uploadId)
    } catch (err) {
      if (!(err instanceof DOMException && err.name === 'AbortError')) {
        setError(setupErrorMessage(err, t('uploadFailed')))
      }
    } finally {
      setUploading(false)
      uploadAbortRef.current = null
    }
  }

  function buildSourceInput(): BSSourceInput {
    if (sourceType === 'bridge') {
      return {
        sourceType: 'bridge' as BSSourceInputSourceType,
        bridgeUrl: bridgeUrl.trim(),
        bridgeToken: bridgeToken.trim(),
        allowInsecureLocal,
        textureSource: 'bridge' as BSSourceInputTextureSource,
        timezone: timezone.trim() || 'Asia/Shanghai',
      }
    }
    return {
      sourceType: 'db' as BSSourceInputSourceType,
      envId: envId || undefined,
      driver,
      rawDsn: rawDsnMode ? rawDsn.trim() : undefined,
      host: rawDsnMode || driver === 'sqlite' ? undefined : host.trim(),
      port: rawDsnMode || driver === 'sqlite' ? undefined : port,
      database: rawDsnMode ? undefined : database.trim(),
      username: rawDsnMode || driver === 'sqlite' ? undefined : username.trim(),
      password: password || undefined,
      prefix,
      passwordMethod,
      salt: salt || undefined,
      timezone: timezone.trim() || 'Asia/Shanghai',
      textureSource: textureSource as BSSourceInputTextureSource,
      texturesDir: textureSource === 'dir' ? texturesDir.trim() : undefined,
      uploadId: textureSource === 'upload' ? uploadId : undefined,
    }
  }

  async function runSourceTest() {
    setTesting(true)
    setError(null)
    try {
      const res = await testSetupBSSource(buildSourceInput())
      setSourceTest(res.data as TestBSSourceResult)
    } catch (err) {
      setError(setupErrorMessage(err, t('sourceTestFailed')))
    } finally {
      setTesting(false)
    }
  }

  async function runDryRun() {
    setStartingDry(true)
    setError(null)
    try {
      const res = await startSetupBSDryRun({ source: buildSourceInput() })
      const started = res.data as StartJobResult
      setDryJob({
        id: started.jobId,
        kind: 'bs_dry_run',
        status: 'running',
        stage: 'dry_run',
        done: 0,
        total: 0,
        percent: 0,
        hasReport: false,
        updatedAt: new Date().toISOString(),
      })
    } catch (err) {
      setError(setupErrorMessage(err, t('dryRunFailed')))
      setStartingDry(false)
    }
  }

  async function runImport() {
    setStartingImport(true)
    setError(null)
    try {
      const needsFallback = (dryJob?.report?.superadmins ?? 0) === 0
      const res = await startSetupBSImport({
        source: buildSourceInput(),
        database: state.envLocked.database ? { useEnv: true } : db,
        storage: state.envLocked.storage ? { useEnv: true } : storage,
        trustedProxies,
        siteUrl: siteUrl.trim() || undefined,
        fallbackAdmin: needsFallback
          ? { email: fallbackEmail.trim(), nickname: fallbackNickname.trim(), password: fallbackPassword }
          : undefined,
      })
      const started = res.data as StartJobResult
      setImportJob({
        id: started.jobId,
        kind: 'bs_import',
        status: 'running',
        stage: 'import',
        done: 0,
        total: 0,
        percent: 0,
        hasReport: false,
        updatedAt: new Date().toISOString(),
      })
    } catch (err) {
      setError(setupErrorMessage(err, t('importStartFailed')))
      setStartingImport(false)
    }
  }

  // Completed / Running live import view
  if (importJob) {
    const running = importJob.status === 'running'
    return (
      <div className="flex flex-col gap-5">
        <div className="flex flex-col gap-1">
          <h2 className="text-lg font-medium">{t('importingTitle')}</h2>
          <p className="text-sm text-muted-foreground">{importJob.message || importJob.stage}</p>
        </div>

        <Progress value={importJob.percent}>
          <ProgressLabel>{importJob.stage}</ProgressLabel>
          <span className="ml-auto text-sm text-muted-foreground tabular-nums">
            {importJob.total > 0 ? `${importJob.done} / ${importJob.total} (${importJob.percent}%)` : `${importJob.percent}%`}
          </span>
        </Progress>

        {importJob.status === 'failed' && (
          <Alert variant="destructive">
            <AlertTitle>{t('importFailedTitle')}</AlertTitle>
            <AlertDescription>{importJob.error}</AlertDescription>
          </Alert>
        )}

        {importJob.report && (
          <BSReportView report={importJob.report} jobId={importJob.id} hasReport={importJob.hasReport} />
        )}

        {importJob.status === 'succeeded' && (
          <div className="flex flex-col gap-4 rounded-xl border bg-card p-5">
            <h3 className="text-sm font-medium">{t('postChecklistTitle')}</h3>
            <ul className="list-disc space-y-1.5 pl-5 text-xs leading-relaxed text-muted-foreground">
              {sourceType === 'bridge' && <li>{t('postCheckBridge')}</li>}
              {sourceType === 'db' && textureSource === 'none' && <li>{t('postCheckNoneTextures')}</li>}
              <li>{t('postCheckPasswords')}</li>
              <li>{t('postCheckCompare')}</li>
            </ul>
            <CopyField value={`skinloom import compare --env ${envPath.trim() || '/path/to/blessing-skin/.env'}`} />
            <div className="flex justify-end">
              <Button onClick={() => onCompleted(importJob.redirectTo || '/auth/login')}>
                {t('enterSite')}
              </Button>
            </div>
          </div>
        )}

        {importJob.status === 'failed' && !running && (
          <div className="flex justify-between">
            <Button variant="outline" onClick={() => setImportJob(null)}>
              {t('backToConfig')}
            </Button>
          </div>
        )}
      </div>
    )
  }

  const dryRunning = Boolean(dryJob && dryJob.status === 'running')
  const dryReport = dryJob?.report
  const needsFallbackAdmin = Boolean(dryReport && dryReport.superadmins === 0)
  const fallbackReady =
    !needsFallbackAdmin ||
    Boolean(fallbackEmail.trim() && fallbackNickname.trim() && fallbackPassword.length >= 8)
  const canExecuteImport = Boolean(dryJob?.status === 'succeeded' && dryReport && !dryReport.blocked && fallbackReady)

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h2 className="text-lg font-medium">{t('title')}</h2>
        <p className="text-sm text-muted-foreground">{t('intro')}</p>
      </div>

      <FormError message={error} />

      <Tabs
        value={sourceType}
        onValueChange={(v) => {
          setSourceType(v as 'db' | 'bridge')
          setSourceTest(null)
          setDryJob(null)
        }}
      >
        <TabsList className="h-auto! w-full flex-wrap justify-start gap-1 sm:w-fit">
          <TabsTrigger value="db" className="py-1">{t('sourceTabs.db')}</TabsTrigger>
          <TabsTrigger value="bridge" className="py-1">{t('sourceTabs.bridge')}</TabsTrigger>
        </TabsList>
      </Tabs>

      {sourceType === 'db' ? (
        <div className="flex flex-col gap-5">
          {/* .env loader */}
          <section className="flex flex-col gap-4 rounded-xl border bg-card p-5">
            <div className="flex items-center gap-2">
              <FileText className="size-4 text-muted-foreground" aria-hidden />
              <h3 className="text-sm font-medium">{t('env.title')}</h3>
            </div>
            <Dropzone
              accept=".env,text/plain"
              disabled={parsingEnv}
              hint={t('env.dropHint')}
              onFile={(f) => void handleEnvFile(f)}
              className="min-h-28"
            />
            <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
              <div className="flex-1">
                <Field
                  id="bs-env-path"
                  label={t('env.pathLabel')}
                  placeholder="/var/www/blessing-skin/.env"
                  value={envPath}
                  onChange={(e) => setEnvPath(e.target.value)}
                />
              </div>
              <Button type="button" variant="outline" loading={parsingEnv} onClick={() => void handleEnvPathParse()}>
                {t('env.loadPath')}
              </Button>
            </div>

            {envInfo?.localhostWarning && (
              <Alert variant="destructive">
                <AlertTitle>{t('env.localhostWarnTitle')}</AlertTitle>
                <AlertDescription>{t('env.localhostWarnDesc')}</AlertDescription>
              </Alert>
            )}

            {envInfo && (
              <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <Badge variant="secondary">{envInfo.driver}</Badge>
                {envInfo.hasSalt && <Badge variant="secondary">SALT</Badge>}
                {envInfo.fsDriver && <Badge variant="outline">FS: {envInfo.fsDriver}</Badge>}
                {envInfo.transition && envInfo.transition.length > 0 && (
                  <Badge variant="outline">{envInfo.transition.join(' → ')}</Badge>
                )}
              </div>
            )}
          </section>

          {/* BS DB & Password settings */}
          <section className="flex flex-col gap-4 rounded-xl border bg-card p-5">
            <h3 className="text-sm font-medium">{t('dbSection.title')}</h3>

            <Tabs
              value={driver}
              onValueChange={(v) => {
                setDriver(v as BSSourceInputDriver)
                setSourceTest(null)
              }}
            >
              <TabsList className="h-auto! w-full flex-wrap justify-start gap-1 sm:w-fit">
                <TabsTrigger value="mysql" className="py-1">MySQL / MariaDB</TabsTrigger>
                <TabsTrigger value="postgres" className="py-1">PostgreSQL</TabsTrigger>
                <TabsTrigger value="sqlite" className="py-1">SQLite</TabsTrigger>
              </TabsList>
            </Tabs>

            {driver === 'sqlite' ? (
              <Field
                id="bs-sqlite-db"
                label={t('dbSection.sqlitePath')}
                value={database}
                onChange={(e) => setDatabase(e.target.value)}
              />
            ) : (
              <>
                <div className="flex items-center gap-2">
                  <Checkbox
                    id="bs-raw-dsn"
                    checked={rawDsnMode}
                    onCheckedChange={(c) => setRawDsnMode(Boolean(c))}
                  />
                  <label htmlFor="bs-raw-dsn" className="cursor-pointer text-xs text-muted-foreground">
                    {t('dbSection.useRawDsn')}
                  </label>
                </div>

                {rawDsnMode ? (
                  <Field id="bs-raw-dsn-in" label="DSN" value={rawDsn} onChange={(e) => setRawDsn(e.target.value)} />
                ) : (
                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    <Field id="bs-host" label={t('dbSection.host')} value={host} onChange={(e) => setHost(e.target.value)} />
                    <Field
                      id="bs-port"
                      label={t('dbSection.port')}
                      type="number"
                      value={port}
                      onChange={(e) => setPort(Number(e.target.value) || 0)}
                    />
                    <Field
                      id="bs-database"
                      label={t('dbSection.database')}
                      value={database}
                      onChange={(e) => setDatabase(e.target.value)}
                    />
                    <Field
                      id="bs-username"
                      label={t('dbSection.username')}
                      value={username}
                      onChange={(e) => setUsername(e.target.value)}
                    />
                    <Field
                      id="bs-password"
                      label={t('dbSection.password')}
                      type="password"
                      autoComplete="new-password"
                      placeholder={envId ? t('secretLoaded') : ''}
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                    />
                    <Field id="bs-prefix" label={t('dbSection.prefix')} value={prefix} onChange={(e) => setPrefix(e.target.value)} />
                  </div>
                )}
              </>
            )}

            <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
              <UiField>
                <FieldLabel>{t('dbSection.pwdMethod')}</FieldLabel>
                <Select value={passwordMethod} onValueChange={(v) => setPasswordMethod(v ?? 'BCRYPT')}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {PWD_METHODS.map((m) => (
                      <SelectItem key={m} value={m}>
                        {m}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </UiField>
              <Field
                id="bs-salt"
                label="SALT"
                placeholder={envInfo?.hasSalt ? t('secretLoaded') : ''}
                value={salt}
                onChange={(e) => setSalt(e.target.value)}
              />
              <Field
                id="bs-tz"
                label={t('dbSection.timezone')}
                value={timezone}
                onChange={(e) => setTimezone(e.target.value)}
              />
            </div>
          </section>

          {/* Texture source */}
          <section className="flex flex-col gap-4 rounded-xl border bg-card p-5">
            <h3 className="text-sm font-medium">{t('textures.title')}</h3>
            <Tabs
              value={textureSource}
              onValueChange={(v) => {
                setTextureSource(v as 'dir' | 'upload' | 'none')
                setSourceTest(null)
              }}
            >
              <TabsList className="h-auto! w-full flex-wrap justify-start gap-1 sm:w-fit">
                <TabsTrigger value="dir" className="py-1">{t('textures.dir')}</TabsTrigger>
                <TabsTrigger value="upload" className="py-1">{t('textures.upload')}</TabsTrigger>
                <TabsTrigger value="none" className="py-1">{t('textures.none')}</TabsTrigger>
              </TabsList>
            </Tabs>

            {textureSource === 'dir' && (
              <Field
                id="bs-textures-dir"
                label={t('textures.dirLabel')}
                hint={t('textures.dirHint')}
                placeholder="/var/www/blessing-skin/storage/textures"
                value={texturesDir}
                onChange={(e) => setTexturesDir(e.target.value)}
              />
            )}

            {textureSource === 'upload' && (
              <div className="flex flex-col gap-3">
                <Dropzone
                  accept=".tar.gz,.tgz,.zip"
                  disabled={uploading}
                  hint={t('textures.uploadHint', { max: formatSetupBytes(state.maxUploadBytes, locale) })}
                  onFile={(f) => void handleTextureArchive(f)}
                >
                  {uploadId || uploading ? (
                    <div className="flex flex-col items-center gap-2">
                      <FileArchive className="size-8 text-primary" aria-hidden />
                      <span className="text-sm font-medium">{uploadName}</span>
                      {uploadId && <span className="text-xs text-muted-foreground">{t('textures.uploadedReady')}</span>}
                    </div>
                  ) : undefined}
                </Dropzone>
                {uploading && uploadProgress && (
                  <div className="flex flex-col gap-2">
                    <Progress value={uploadProgress.total > 0 ? Math.round((uploadProgress.loaded / uploadProgress.total) * 100) : 0}>
                      <ProgressLabel>{t('textures.uploading')}</ProgressLabel>
                      <span className="ml-auto text-sm text-muted-foreground tabular-nums">
                        {formatSetupBytes(uploadProgress.loaded, locale)} / {formatSetupBytes(uploadProgress.total, locale)}
                      </span>
                    </Progress>
                    <Button variant="outline" size="xs" className="self-end" onClick={() => uploadAbortRef.current?.abort()}>
                      {t('cancelUpload')}
                    </Button>
                  </div>
                )}
              </div>
            )}

            {textureSource === 'none' && (
              <Alert>
                <AlertDescription>{t('textures.noneDesc')}</AlertDescription>
              </Alert>
            )}
          </section>
        </div>
      ) : (
        <section className="flex flex-col gap-4 rounded-xl border bg-card p-5">
          <div className="flex flex-col gap-1">
            <h3 className="text-sm font-medium">{t('bridge.title')}</h3>
            <p className="text-xs text-muted-foreground">{t('bridge.intro')}</p>
          </div>
          <Field
            id="bs-bridge-url"
            label={t('bridge.urlLabel')}
            hint={t('bridge.urlHint')}
            placeholder="https://skins.example.com"
            value={bridgeUrl}
            onChange={(e) => {
              const raw = e.target.value
              const trimmed = raw.trim()
              if (trimmed.startsWith('{') && trimmed.endsWith('}')) {
                try {
                  const parsed = JSON.parse(trimmed) as { url?: string; token?: string }
                  if (typeof parsed.url === 'string' && parsed.url.trim()) {
                    setBridgeUrl(parsed.url.trim())
                    if (typeof parsed.token === 'string' && parsed.token.trim()) {
                      setBridgeToken(parsed.token.trim())
                    }
                    return
                  }
                } catch {
                  // Fall back to regular URL input
                }
              }
              setBridgeUrl(raw)
            }}
          />
          <Field
            id="bs-bridge-token"
            label={t('bridge.tokenLabel')}
            type="password"
            autoComplete="off"
            value={bridgeToken}
            onChange={(e) => setBridgeToken(e.target.value)}
          />
          <label className="flex cursor-pointer items-center gap-2 text-xs">
            <Checkbox
              checked={allowInsecureLocal}
              onCheckedChange={(c) => setAllowInsecureLocal(Boolean(c))}
            />
            <span>{t('bridge.allowInsecure')}</span>
          </label>
          <Field
            id="bs-bridge-tz"
            label={t('dbSection.timezone')}
            value={timezone}
            onChange={(e) => setTimezone(e.target.value)}
          />
        </section>
      )}

      {/* Test + Dry run action bar */}
      <div className="flex flex-wrap items-center gap-3">
        <Button variant="outline" loading={testing} onClick={() => void runSourceTest()}>
          {t('actions.testSource')}
        </Button>
        <Button variant="secondary" loading={startingDry || dryRunning} onClick={() => void runDryRun()}>
          {t('actions.dryRun')}
        </Button>
      </div>

      {sourceTest && (
        <Alert variant={sourceTest.ok ? 'default' : 'destructive'}>
          <AlertTitle>{sourceTest.ok ? t('sourceOkTitle') : t('sourceFailTitle')}</AlertTitle>
          <AlertDescription className="flex flex-col gap-1 text-xs">
            {sourceTest.ok ? (
              <>
                <span>
                  {t('sourceCounts', {
                    users: sourceTest.counts.users,
                    players: sourceTest.counts.players,
                    textures: sourceTest.counts.textures,
                    closet: sourceTest.counts.closet,
                  })}
                </span>
                {sourceTest.counts.textureDirStats.exists && (
                  <span>
                    {t('textureDirCounts', {
                      files: sourceTest.counts.textureDirStats.files,
                      size: formatSetupBytes(sourceTest.counts.textureDirStats.bytes, locale),
                    })}
                  </span>
                )}
                {sourceTest.warning && <span className="text-muted-foreground">{sourceTest.warning}</span>}
              </>
            ) : (
              <span className="font-mono break-all">{sourceTest.errorDetail || t('sourceTestFailed')}</span>
            )}
          </AlertDescription>
        </Alert>
      )}

      {dryRunning && dryJob && (
        <Progress value={dryJob.percent}>
          <ProgressLabel>{dryJob.message || dryJob.stage}</ProgressLabel>
          <span className="ml-auto text-sm text-muted-foreground tabular-nums">{dryJob.percent}%</span>
        </Progress>
      )}

      {dryJob?.status === 'failed' && (
        <Alert variant="destructive">
          <AlertTitle>{t('dryRunFailedTitle')}</AlertTitle>
          <AlertDescription>{dryJob.error}</AlertDescription>
        </Alert>
      )}

      {dryReport && (
        <BSReportView report={dryReport} jobId={dryJob!.id} hasReport={dryJob?.hasReport} />
      )}

      {canExecuteImport && (
        <section className="flex flex-col gap-4 rounded-xl border bg-card p-5">
          <h3 className="text-sm font-medium">{t('executeSection.title')}</h3>
          <Field
            id="bs-target-site-url"
            label={t('executeSection.siteUrlLabel')}
            hint={t('executeSection.siteUrlHint')}
            type="url"
            value={siteUrl}
            onChange={(e) => setSiteUrl(e.target.value)}
          />
          {(dryReport?.superadmins ?? 0) === 0 && (
            <div className="flex flex-col gap-3 rounded-lg border bg-muted/30 p-4">
              <p className="text-xs text-muted-foreground">{t('executeSection.fallbackDesc')}</p>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <Field
                  id="bs-fallback-email"
                  label={t('executeSection.fallbackEmail')}
                  type="email"
                  value={fallbackEmail}
                  onChange={(e) => setFallbackEmail(e.target.value)}
                  required
                />
                <Field
                  id="bs-fallback-nick"
                  label={t('executeSection.fallbackNick')}
                  value={fallbackNickname}
                  onChange={(e) => setFallbackNickname(e.target.value)}
                  required
                />
              </div>
              <Field
                id="bs-fallback-pass"
                label={t('executeSection.fallbackPass')}
                type="password"
                autoComplete="new-password"
                value={fallbackPassword}
                onChange={(e) => setFallbackPassword(e.target.value)}
                required
              />
            </div>
          )}
        </section>
      )}

      <Separator />

      <div className="flex flex-wrap items-center justify-between gap-3">
        <Button variant="outline" disabled={dryRunning || startingImport} onClick={onBack}>
          {t('actions.back')}
        </Button>
        <Button disabled={!canExecuteImport} loading={startingImport} onClick={() => void runImport()}>
          {t('actions.startImport')}
        </Button>
      </div>
    </div>
  )
}
