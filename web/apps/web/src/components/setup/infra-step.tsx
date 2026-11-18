'use client'

import {
  connectExistingSetup,
  type DBConfigInput,
  type SetupState,
  type StorageConfigInput,
  type TestDBResult,
  type TestStorageResult,
  testSetupDB,
  testSetupStorage,
} from '@skinloom/api-client'
import {
  Alert,
  AlertDescription,
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertTitle,
  Badge,
  Button,
  Checkbox,
  CodeBlock,
  Field as UiField,
  FieldLabel,
  Input,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Separator,
  StatusDot,
  Tabs,
  TabsList,
  TabsTrigger,
  toast,
} from '@skinloom/ui'
import { Database, HardDrive, Lock, ShieldCheck } from 'lucide-react'
import { useTranslations } from 'next-intl'
import { useState } from 'react'

import { Field, FormError } from '@/components/auth/field'

import { setupErrorMessage } from './types'

const S3_PRESETS: {
  id: string
  region: string
  pathStyle: boolean
  endpointHint: string
  insecure?: boolean
}[] = [
  { id: 'aliyun', region: 'cn-hangzhou', pathStyle: false, endpointHint: 'oss-cn-hangzhou.aliyuncs.com' },
  { id: 'tencent', region: 'ap-guangzhou', pathStyle: false, endpointHint: 'cos.ap-guangzhou.myqcloud.com' },
  { id: 'r2', region: 'auto', pathStyle: true, endpointHint: '<account_id>.r2.cloudflarestorage.com' },
  { id: 'minio', region: 'us-east-1', pathStyle: true, endpointHint: '127.0.0.1:9000', insecure: true },
]

export function InfraStep({
  state,
  db,
  storage,
  trustProxy,
  onChangeDB,
  onChangeStorage,
  onChangeTrustProxy,
  onSwitchToBS,
  onBack,
  onNext,
  onConnectedExisting,
}: {
  state: SetupState
  db: DBConfigInput
  storage: StorageConfigInput
  trustProxy: boolean
  onChangeDB: (db: DBConfigInput) => void
  onChangeStorage: (storage: StorageConfigInput) => void
  onChangeTrustProxy: (v: boolean) => void
  onSwitchToBS: () => void
  onBack: () => void
  onNext: () => void
  onConnectedExisting: (redirectTo: string) => void
}) {
  const t = useTranslations('setup.infra')
  const [rawDsnMode, setRawDsnMode] = useState(Boolean(db.rawDsn))
  const [dbTesting, setDbTesting] = useState(false)
  const [dbProbe, setDbProbe] = useState<TestDBResult | null>(null)
  const [storageTesting, setStorageTesting] = useState(false)
  const [storageProbe, setStorageProbe] = useState<TestStorageResult | null>(null)
  const [connecting, setConnecting] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [advancing, setAdvancing] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const trustedProxies = state.envLocked.trustedProxies
    ? state.trustedProxies
    : trustProxy && state.detectedProxy?.suggestedCidr
      ? Array.from(new Set([...state.trustedProxies, state.detectedProxy.suggestedCidr]))
      : state.trustedProxies.filter((p) => p !== state.detectedProxy?.suggestedCidr)

  function updateDB(patch: Partial<DBConfigInput>) {
    setDbProbe(null)
    onChangeDB({ ...db, ...patch })
  }

  function switchDBDriver(driver: 'sqlite' | 'mysql' | 'postgres') {
    setDbProbe(null)
    setRawDsnMode(false)
    if (driver === 'sqlite') {
      onChangeDB({ driver: 'sqlite', sqlitePath: db.sqlitePath || 'skinloom.db' })
    } else if (driver === 'mysql') {
      onChangeDB({
        driver: 'mysql',
        host: db.host || '127.0.0.1',
        port: 3306,
        database: db.database || 'skinloom',
        username: db.username || 'skinloom',
        password: db.password || '',
      })
    } else {
      onChangeDB({
        driver: 'postgres',
        host: db.host || '127.0.0.1',
        port: 5432,
        database: db.database || 'skinloom',
        username: db.username || 'skinloom',
        password: db.password || '',
        sslMode: db.sslMode || 'disable',
      })
    }
  }

  function updateStorage(patch: Partial<StorageConfigInput>) {
    setStorageProbe(null)
    onChangeStorage({ ...storage, ...patch })
  }

  async function runDBTest(): Promise<TestDBResult | null> {
    setDbTesting(true)
    setError(null)
    try {
      const payload: DBConfigInput = state.envLocked.database ? { useEnv: true } : db
      const res = await testSetupDB(payload)
      const data = res.data as TestDBResult
      setDbProbe(data)
      return data
    } catch (err) {
      setError(setupErrorMessage(err, t('dbTestFailed')))
      return null
    } finally {
      setDbTesting(false)
    }
  }

  async function runStorageTest(): Promise<TestStorageResult | null> {
    setStorageTesting(true)
    setError(null)
    try {
      const payload: StorageConfigInput = state.envLocked.storage ? { useEnv: true } : storage
      const res = await testSetupStorage(payload)
      const data = res.data as TestStorageResult
      setStorageProbe(data)
      return data
    } catch (err) {
      setError(setupErrorMessage(err, t('storageTestFailed')))
      return null
    } finally {
      setStorageTesting(false)
    }
  }

  async function handleNext() {
    setAdvancing(true)
    setError(null)
    try {
      const dbRes = dbProbe ?? (await runDBTest())
      if (!dbRes || !dbRes.ok || dbRes.state !== 'empty') {
        return
      }
      const stRes = storageProbe ?? (await runStorageTest())
      if (!stRes || !stRes.ok) {
        return
      }
      onNext()
    } finally {
      setAdvancing(false)
    }
  }

  async function handleConnectExisting() {
    setConnecting(true)
    setError(null)
    try {
      const res = await connectExistingSetup({
        database: state.envLocked.database ? { useEnv: true } : db,
        storage: state.envLocked.storage ? { useEnv: true } : storage,
        trustedProxies,
      })
      const data = res.data as { redirectTo: string }
      onConnectedExisting(data.redirectTo || '/auth/login')
    } catch (err) {
      setConfirmOpen(false)
      setError(setupErrorMessage(err, t('connectFailed')))
      setConnecting(false)
    }
  }

  const driver = db.driver === 'mysql' || db.driver === 'postgres' ? db.driver : 'sqlite'
  const storageDriver = storage.driver === 's3' ? 's3' : 'local'

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h2 className="text-lg font-medium">{t('title')}</h2>
        <p className="text-sm text-muted-foreground">{t('intro')}</p>
      </div>

      {state.initDbError && (
        <Alert variant="destructive">
          <AlertTitle>{t('initDbErrorTitle')}</AlertTitle>
          <AlertDescription>{state.initDbError}</AlertDescription>
        </Alert>
      )}

      <FormError message={error} />

      {/* Database */}
      <section className="flex flex-col gap-4 rounded-xl border bg-card p-5">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <Database className="size-4 text-muted-foreground" aria-hidden />
            <h3 className="text-sm font-medium">{t('db.title')}</h3>
          </div>
          {state.envLocked.database && (
            <Badge variant="secondary" className="gap-1">
              <Lock className="size-3" aria-hidden />
              {t('envLocked')}
            </Badge>
          )}
        </div>

        {state.envLocked.database ? (
          <div className="flex flex-col gap-2 rounded-lg bg-muted/40 p-3 text-sm">
            <div className="flex items-center justify-between gap-2">
              <span className="text-muted-foreground">{t('db.driver')}</span>
              <span className="font-mono text-caption">{state.envLocked.databaseDriver}</span>
            </div>
            {state.envLocked.databaseMask && (
              <div className="flex items-center justify-between gap-2">
                <span className="text-muted-foreground">DSN</span>
                <span className="truncate font-mono text-caption">{state.envLocked.databaseMask}</span>
              </div>
            )}
          </div>
        ) : (
          <div className="flex flex-col gap-4">
            <Tabs value={driver} onValueChange={(v) => switchDBDriver(v as 'sqlite' | 'mysql' | 'postgres')}>
              <TabsList className="h-auto! w-full flex-wrap justify-start gap-1 sm:w-fit">
                <TabsTrigger value="sqlite" className="py-1">SQLite</TabsTrigger>
                <TabsTrigger value="mysql" className="py-1">MySQL / MariaDB</TabsTrigger>
                <TabsTrigger value="postgres" className="py-1">PostgreSQL</TabsTrigger>
              </TabsList>
            </Tabs>

            {driver === 'sqlite' ? (
              <Field
                id="db-sqlite-path"
                label={t('db.sqlitePath')}
                hint={t('db.sqliteHint')}
                value={db.sqlitePath ?? 'skinloom.db'}
                onChange={(e) => updateDB({ sqlitePath: e.target.value })}
              />
            ) : (
              <>
                <div className="flex items-center gap-2">
                  <Checkbox
                    id="db-raw-dsn"
                    checked={rawDsnMode}
                    onCheckedChange={(checked) => {
                      setRawDsnMode(Boolean(checked))
                      if (!checked) updateDB({ rawDsn: '' })
                    }}
                  />
                  <label htmlFor="db-raw-dsn" className="cursor-pointer text-xs text-muted-foreground">
                    {t('db.useRawDsn')}
                  </label>
                </div>

                {rawDsnMode ? (
                  <Field
                    id="db-raw-dsn-input"
                    label="DSN"
                    hint={driver === 'mysql' ? 'user:pass@tcp(127.0.0.1:3306)/skinloom?parseTime=true' : 'postgres://user:pass@127.0.0.1:5432/skinloom?sslmode=disable'}
                    value={db.rawDsn ?? ''}
                    onChange={(e) => updateDB({ rawDsn: e.target.value })}
                  />
                ) : (
                  <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                    <Field
                      id="db-host"
                      label={t('db.host')}
                      value={db.host ?? ''}
                      onChange={(e) => updateDB({ host: e.target.value })}
                    />
                    <Field
                      id="db-port"
                      label={t('db.port')}
                      type="number"
                      value={db.port ?? (driver === 'mysql' ? 3306 : 5432)}
                      onChange={(e) => updateDB({ port: Number(e.target.value) || 0 })}
                    />
                    <Field
                      id="db-name"
                      label={t('db.database')}
                      value={db.database ?? ''}
                      onChange={(e) => updateDB({ database: e.target.value })}
                    />
                    <Field
                      id="db-user"
                      label={t('db.username')}
                      value={db.username ?? ''}
                      onChange={(e) => updateDB({ username: e.target.value })}
                    />
                    <Field
                      id="db-pass"
                      label={t('db.password')}
                      type="password"
                      autoComplete="new-password"
                      value={db.password ?? ''}
                      onChange={(e) => updateDB({ password: e.target.value })}
                    />
                    {driver === 'postgres' && (
                      <UiField>
                        <FieldLabel>{t('db.sslMode')}</FieldLabel>
                        <Select
                          value={db.sslMode || 'disable'}
                          onValueChange={(v) => updateDB({ sslMode: v ?? 'disable' })}
                        >
                          <SelectTrigger className="w-full">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="disable">disable</SelectItem>
                            <SelectItem value="require">require</SelectItem>
                            <SelectItem value="verify-ca">verify-ca</SelectItem>
                            <SelectItem value="verify-full">verify-full</SelectItem>
                          </SelectContent>
                        </Select>
                      </UiField>
                    )}
                  </div>
                )}
              </>
            )}
          </div>
        )}

        <div className="flex flex-wrap items-center gap-3">
          <Button variant="outline" size="sm" loading={dbTesting} onClick={() => void runDBTest()}>
            {t('db.test')}
          </Button>
          {dbProbe && (
            <StatusDot
              status={
                !dbProbe.ok || dbProbe.state === 'error'
                  ? 'error'
                  : dbProbe.state === 'empty'
                    ? 'ok'
                    : 'warn'
              }
            >
              {dbProbe.summary || dbProbe.errorDetail}
              {dbProbe.version ? ` (${dbProbe.dialect} ${dbProbe.version})` : ''}
            </StatusDot>
          )}
        </div>

        {dbProbe?.state === 'missing_db' && dbProbe.hintSql && (
          <div className="flex flex-col gap-2">
            <p className="text-xs text-muted-foreground">{t('db.missingHint')}</p>
            <CodeBlock code={dbProbe.hintSql} />
          </div>
        )}

        {dbProbe?.state === 'error' && dbProbe.errorDetail && (
          <Alert variant="destructive">
            <AlertDescription className="font-mono text-xs break-all">{dbProbe.errorDetail}</AlertDescription>
          </Alert>
        )}

        {dbProbe?.state === 'suspected_bs' && (
          <Alert variant="destructive">
            <AlertTitle>{t('db.suspectedBsTitle')}</AlertTitle>
            <AlertDescription className="flex flex-col gap-3">
              <span>{t('db.suspectedBsDesc')}</span>
              <div>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    onSwitchToBS()
                    setDbProbe(null)
                    toast.success(t('db.switchedToBs'))
                  }}
                >
                  {t('db.switchToBs')}
                </Button>
              </div>
            </AlertDescription>
          </Alert>
        )}

        {dbProbe?.state === 'skinloom_installed' && (
          <Alert>
            <AlertTitle>{t('db.installedTitle')}</AlertTitle>
            <AlertDescription className="flex flex-col gap-3">
              <span>
                {t('db.installedDesc', {
                  users: dbProbe.users ?? 0,
                  textures: dbProbe.textures ?? 0,
                  players: dbProbe.players ?? 0,
                })}
              </span>
              <div>
                <Button variant="outline" size="sm" onClick={() => setConfirmOpen(true)}>
                  {t('db.connectExisting')}
                </Button>
              </div>
            </AlertDescription>
          </Alert>
        )}
      </section>

      {/* Storage */}
      <section className="flex flex-col gap-4 rounded-xl border bg-card p-5">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <HardDrive className="size-4 text-muted-foreground" aria-hidden />
            <h3 className="text-sm font-medium">{t('storage.title')}</h3>
          </div>
          {state.envLocked.storage && (
            <Badge variant="secondary" className="gap-1">
              <Lock className="size-3" aria-hidden />
              {t('envLocked')}
            </Badge>
          )}
        </div>

        {state.envLocked.storage ? (
          <div className="flex flex-col gap-2 rounded-lg bg-muted/40 p-3 text-sm">
            <div className="flex items-center justify-between gap-2">
              <span className="text-muted-foreground">{t('storage.driver')}</span>
              <span className="font-mono text-caption">{state.envLocked.storageDriver}</span>
            </div>
            {state.envLocked.storageMask && (
              <div className="flex items-center justify-between gap-2">
                <span className="text-muted-foreground">{t('storage.target')}</span>
                <span className="truncate font-mono text-caption">{state.envLocked.storageMask}</span>
              </div>
            )}
          </div>
        ) : (
          <div className="flex flex-col gap-4">
            <Tabs value={storageDriver} onValueChange={(v) => updateStorage({ driver: v as 'local' | 's3' })}>
              <TabsList className="h-auto! w-full flex-wrap justify-start gap-1 sm:w-fit">
                <TabsTrigger value="local" className="py-1">{t('storage.local')}</TabsTrigger>
                <TabsTrigger value="s3" className="py-1">{t('storage.s3')}</TabsTrigger>
              </TabsList>
            </Tabs>

            {storageDriver === 'local' ? (
              <Field
                id="storage-local-path"
                label={t('storage.localPath')}
                hint={t('storage.localHint')}
                value={storage.path ?? 'textures'}
                onChange={(e) => updateStorage({ path: e.target.value })}
              />
            ) : (
              <div className="flex flex-col gap-4">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-xs text-muted-foreground">{t('storage.presets')}:</span>
                  {S3_PRESETS.map((p) => (
                    <Button
                      key={p.id}
                      type="button"
                      variant="outline"
                      size="xs"
                      onClick={() =>
                        updateStorage({
                          driver: 's3',
                          region: p.region,
                          pathStyle: p.pathStyle,
                          insecure: p.insecure ?? false,
                          endpoint: storage.endpoint || p.endpointHint,
                        })
                      }
                    >
                      {t(`storage.presetNames.${p.id}`)}
                    </Button>
                  ))}
                </div>

                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <Field
                    id="s3-endpoint"
                    label="Endpoint"
                    hint={t('storage.endpointHint')}
                    value={storage.endpoint ?? ''}
                    onChange={(e) => updateStorage({ endpoint: e.target.value })}
                  />
                  <Field
                    id="s3-bucket"
                    label="Bucket"
                    value={storage.bucket ?? ''}
                    onChange={(e) => updateStorage({ bucket: e.target.value })}
                  />
                  <Field
                    id="s3-region"
                    label="Region"
                    value={storage.region ?? ''}
                    onChange={(e) => updateStorage({ region: e.target.value })}
                  />
                  <Field
                    id="s3-prefix"
                    label={t('storage.prefix')}
                    value={storage.prefix ?? ''}
                    onChange={(e) => updateStorage({ prefix: e.target.value })}
                  />
                  <Field
                    id="s3-ak"
                    label="Access Key"
                    value={storage.accessKey ?? ''}
                    onChange={(e) => updateStorage({ accessKey: e.target.value })}
                  />
                  <Field
                    id="s3-sk"
                    label="Secret Key"
                    type="password"
                    autoComplete="new-password"
                    value={storage.secretKey ?? ''}
                    onChange={(e) => updateStorage({ secretKey: e.target.value })}
                  />
                </div>
                <Field
                  id="s3-public-url"
                  label={t('storage.publicUrl')}
                  hint={t('storage.publicUrlHint')}
                  value={storage.publicUrl ?? ''}
                  onChange={(e) => updateStorage({ publicUrl: e.target.value })}
                />
                <div className="flex flex-wrap gap-5">
                  <label className="flex cursor-pointer items-center gap-2 text-xs">
                    <Checkbox
                      checked={Boolean(storage.pathStyle)}
                      onCheckedChange={(checked) => updateStorage({ pathStyle: Boolean(checked) })}
                    />
                    <span>{t('storage.pathStyle')}</span>
                  </label>
                  <label className="flex cursor-pointer items-center gap-2 text-xs">
                    <Checkbox
                      checked={Boolean(storage.insecure)}
                      onCheckedChange={(checked) => updateStorage({ insecure: Boolean(checked) })}
                    />
                    <span>{t('storage.insecure')}</span>
                  </label>
                </div>
              </div>
            )}
          </div>
        )}

        <div className="flex flex-wrap items-center gap-3">
          <Button variant="outline" size="sm" loading={storageTesting} onClick={() => void runStorageTest()}>
            {t('storage.test')}
          </Button>
          {storageProbe && (
            <StatusDot status={storageProbe.ok ? 'ok' : 'error'}>
              {storageProbe.ok ? storageProbe.summary : storageProbe.errorDetail ?? storageProbe.summary}
            </StatusDot>
          )}
        </div>
      </section>

      {/* Detected reverse proxy */}
      {(state.detectedProxy || state.envLocked.trustedProxies || state.trustedProxies.length > 0) && (
        <section className="flex flex-col gap-3 rounded-xl border bg-card p-5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <ShieldCheck className="size-4 text-muted-foreground" aria-hidden />
              <h3 className="text-sm font-medium">{t('proxy.title')}</h3>
            </div>
            {state.envLocked.trustedProxies && (
              <Badge variant="secondary" className="gap-1">
                <Lock className="size-3" aria-hidden />
                {t('envLocked')}
              </Badge>
            )}
          </div>
          {state.detectedProxy && !state.envLocked.trustedProxies && (
            <label className="flex cursor-pointer items-start gap-2.5 text-sm">
              <Checkbox
                className="mt-0.5"
                checked={trustProxy}
                onCheckedChange={(checked) => onChangeTrustProxy(Boolean(checked))}
              />
              <span className="flex flex-col gap-0.5">
                <span>{t('proxy.trust', { cidr: state.detectedProxy.suggestedCidr })}</span>
                <span className="text-xs text-muted-foreground">
                  {t('proxy.detected', {
                    peer: state.detectedProxy.peerIp,
                    header: `${state.detectedProxy.proto ?? 'http'}://${state.detectedProxy.host ?? ''}`,
                  })}
                </span>
              </span>
            </label>
          )}
          {state.trustedProxies.length > 0 && (
            <p className="font-mono text-xs text-muted-foreground">
              trusted_proxies: {trustedProxies.join(', ') || '—'}
            </p>
          )}
        </section>
      )}

      <Separator />

      <div className="flex flex-wrap items-center justify-between gap-3">
        <Button variant="outline" onClick={onBack}>
          {t('back')}
        </Button>
        <Button loading={advancing} onClick={() => void handleNext()}>
          {t('next')}
        </Button>
      </div>

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('db.connectDialogTitle')}</AlertDialogTitle>
            <AlertDialogDescription>{t('db.connectDialogDesc')}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={connecting}>{t('cancel')}</AlertDialogCancel>
            <AlertDialogAction loading={connecting} onClick={() => void handleConnectExisting()}>
              {t('db.connectExistingConfirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
