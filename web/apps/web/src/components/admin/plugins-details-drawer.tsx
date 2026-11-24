'use client'

import { type AdminPluginView, getPlugin } from '@skinloom/api-client'
import {
  Alert,
  AlertAction,
  AlertDescription,
  Badge,
  Button,
  CopyField,
  RelativeTime,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  Skeleton,
  StatusDot,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from '@skinloom/ui'
import { Box, CircleAlert, Cpu, ExternalLink } from 'lucide-react'
import { useLocale, useTranslations } from 'next-intl'
import { useCallback, useEffect, useRef, useState } from 'react'

import { formatBytes } from '@/lib/bytes'
import { problemOf } from '@/lib/problem'
import { safeHref } from '@/lib/safe-url'

import { apiProblemMessage, formatDuration, permissionMessageKey } from './plugins-settings-form'

export function PluginDetailsDrawer({
  plugin: initialPlugin,
  open,
  onOpenChange,
}: {
  plugin: AdminPluginView | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const t = useTranslations('adminPlugins.detailsDrawer')
  const te = useTranslations('adminPlugins')
  const ti = useTranslations('adminPlugins.inspect')
  const tc = useTranslations('common')
  const locale = useLocale()
  const loadErrRef = useRef(te('errors.load'))
  loadErrRef.current = te('errors.load')
  const [detail, setDetail] = useState<AdminPluginView | null>(initialPlugin)
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const pluginId = initialPlugin?.id ?? null
  const seqRef = useRef(0)
  const abortRef = useRef<AbortController | null>(null)

  const load = useCallback(() => {
    abortRef.current?.abort()
    const seq = ++seqRef.current
    setLoadError(null)
    if (!open || !pluginId) {
      setLoading(false)
      return
    }
    const ctl = new AbortController()
    abortRef.current = ctl
    setLoading(true)
    getPlugin(pluginId, { signal: ctl.signal })
      .then((res) => {
        if (seq === seqRef.current && res.status === 200) setDetail(res.data as AdminPluginView)
      })
      .catch((err) => {
        if (ctl.signal.aborted || seq !== seqRef.current) return
        setLoadError(apiProblemMessage(problemOf(err).detail, err, loadErrRef.current))
      })
      .finally(() => {
        if (seq === seqRef.current) setLoading(false)
      })
  }, [open, pluginId])

  useEffect(() => {
    void locale
    setDetail(initialPlugin)
    load()
    return () => abortRef.current?.abort()
  }, [initialPlugin, load, locale])

  const logs = detail?.recentLogs ?? []
  const routes = detail?.routes ?? []
  const permissions = detail?.grantedPermissions ?? []
  const permissionMeta = new Map(
    [
      ...(detail?.permissions?.normal ?? []),
      ...(detail?.permissions?.sensitive ?? []),
      ...(detail?.permissions?.high_risk ?? []),
    ].map((item) => [item.name, item]),
  )
  const isNative = detail?.runtime === 'native'
  const homepage = detail?.homepage ? safeHref(detail.homepage, '') : ''

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="flex w-full flex-col overflow-hidden data-[side=right]:w-full sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>{detail ? t('title', { name: detail.name }) : ''}</SheetTitle>
          <SheetDescription className="flex flex-wrap items-center gap-2 font-mono">
            <span className="min-w-0 break-all">{detail ? `${detail.id} v${detail.version}` : ''}</span>
            {detail && detail.kind === 'plugin' ? (
              <Badge
                variant="outline"
                className={
                  isNative
                    ? 'gap-1 border-primary/40 bg-primary/10 font-sans text-primary'
                    : 'gap-1 font-sans text-muted-foreground'
                }
              >
                {isNative ? <Cpu className="size-3" aria-hidden /> : <Box className="size-3" aria-hidden />}
                {te(`runtimes.${isNative ? 'native' : 'js'}`)}
              </Badge>
            ) : null}
          </SheetDescription>
        </SheetHeader>

        {detail ? (
          <div className="flex-1 overflow-y-auto px-4 pb-[max(1.5rem,env(safe-area-inset-bottom))]">
            {loading ? (
              <div className="mb-3 space-y-2">
                <Skeleton className="h-4 w-40" />
                <Skeleton className="h-20 rounded-lg" />
              </div>
            ) : null}
            {loadError ? (
              <Alert variant="destructive" role="alert" className="mb-3">
                <CircleAlert />
                <AlertDescription>{loadError}</AlertDescription>
                <AlertAction>
                  <Button type="button" variant="outline" size="sm" onClick={load}>
                    {tc('retry')}
                  </Button>
                </AlertAction>
              </Alert>
            ) : null}
            <Tabs key={detail.id} defaultValue="overview" className="space-y-4">
              <TabsList variant="line" className="no-scrollbar h-auto w-full justify-start overflow-x-auto overflow-y-hidden pb-1.5">
                <TabsTrigger value="overview">{t('tabs.overview')}</TabsTrigger>
                <TabsTrigger value="logs" className="tabular-nums">
                  {t('tabs.logs')} ({logs.length})
                </TabsTrigger>
                <TabsTrigger value="routes" className="tabular-nums">
                  {t('tabs.routes')} ({routes.length})
                </TabsTrigger>
                <TabsTrigger value="permissions" className="tabular-nums">
                  {t('tabs.permissions')} ({permissions.length})
                </TabsTrigger>
              </TabsList>

              <TabsContent value="overview" className="space-y-4">
                {(detail.authors?.length || homepage || detail.packageSha256) ? (
                  <div className="space-y-3 rounded-lg border border-border p-3">
                    <p className="text-xs font-medium text-muted-foreground">{t('metaTitle')}</p>
                    <div className="space-y-2.5 text-sm">
                      {detail.authors && detail.authors.length > 0 ? (
                        <Metric label={t('metaAuthors')} value={detail.authors.join(', ')} />
                      ) : null}
                      {homepage ? (
                        <div className="min-w-0">
                          <div className="text-xs text-muted-foreground">{t('metaHomepage')}</div>
                          <a
                            href={homepage}
                            target="_blank"
                            rel="noreferrer"
                            className="inline-flex max-w-full items-center gap-1 break-all text-sm font-medium text-primary underline-offset-4 hover:underline"
                          >
                            <span className="truncate">{homepage}</span>
                            <ExternalLink className="size-3.5 shrink-0" aria-hidden />
                          </a>
                        </div>
                      ) : null}
                      {detail.packageSha256 ? (
                        <div className="space-y-1">
                          <div className="text-xs text-muted-foreground">{t('metaSha256')}</div>
                          <CopyField value={detail.packageSha256} label={t('metaSha256')} />
                        </div>
                      ) : null}
                    </div>
                  </div>
                ) : null}

                {detail.nativeStats ? (
                  <div className="space-y-3 rounded-lg border border-border p-3">
                    <div className="flex items-center justify-between gap-2">
                      <p className="text-xs font-medium text-muted-foreground">{t('nativeTitle')}</p>
                      <StatusDot status={detail.nativeStats.running ? 'ok' : 'off'}>
                        {detail.nativeStats.running ? t('nativeRunning') : t('nativeStopped')}
                      </StatusDot>
                    </div>
                    <div className="grid grid-cols-2 gap-3 text-sm">
                      <Metric
                        label={t('nativePid')}
                        value={detail.nativeStats.pid > 0 ? String(detail.nativeStats.pid) : '—'}
                      />
                      <Metric
                        label={t('nativeUptime')}
                        value={detail.nativeStats.running ? formatDuration(detail.nativeStats.uptimeSeconds) : '—'}
                      />
                      <Metric label={t('nativeRestarts')} value={String(detail.nativeStats.restarts)} />
                      <Metric
                        label={t('nativeMemory')}
                        value={
                          detail.nativeStats.memoryRssBytes && detail.nativeStats.memoryRssBytes > 0
                            ? formatBytes(detail.nativeStats.memoryRssBytes, locale)
                            : '—'
                        }
                      />
                      <Metric
                        label={t('nativeDbDriver')}
                        value={detail.nativeStats.dbDriver ? detail.nativeStats.dbDriver.toUpperCase() : t('nativeDbUnused')}
                      />
                      <Metric
                        label={t('nativeDbPrefix')}
                        value={detail.nativeStats.dbPrefix || '—'}
                      />
                      <Metric
                        label={t('nativeProxy')}
                        value={detail.nativeStats.httpProxy ? t('nativeProxyEnabled') : t('nativeProxyDisabled')}
                      />
                    </div>
                  </div>
                ) : null}

                {detail.stats ? (
                  <div className="space-y-2 rounded-lg border border-border p-3">
                    <p className="text-xs font-medium text-muted-foreground">{t('poolTitle')}</p>
                    <div className="grid grid-cols-2 gap-3 text-sm">
                      <Metric label={t('poolInstances')} value={`${detail.stats.idle} / ${detail.stats.size}`} />
                      <Metric label={t('poolInUse')} value={String(detail.stats.inUse)} />
                      <Metric label={t('poolTimeouts')} value={String(detail.stats.timeouts)} />
                      <Metric label={t('poolOoms')} value={String(detail.stats.ooms)} />
                      <Metric label={t('poolRecreated')} value={String(detail.stats.recreated)} />
                    </div>
                  </div>
                ) : null}

                {detail.usage ? (
                  <div className="space-y-2 rounded-lg border border-border p-3">
                    <p className="text-xs font-medium text-muted-foreground">{t('usageTitle')}</p>
                    <div className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-3">
                      <Metric label={t('usageKv')} value={String(detail.usage.kvKeys)} />
                      <Metric label={t('usageDocs')} value={String(detail.usage.docs)} />
                      <Metric label={t('usageFiles')} value={formatBytes(detail.usage.filesBytes, locale)} />
                    </div>
                  </div>
                ) : null}

                {detail.error ? (
                  <div className="rounded-lg border border-destructive/40 bg-destructive/10 p-3 text-xs text-destructive">
                    <pre className="min-w-0 break-all whitespace-pre-wrap font-mono">{detail.error}</pre>
                  </div>
                ) : null}
              </TabsContent>

              <TabsContent value="logs" className="space-y-2">
                {logs.length === 0 ? (
                  <p className="text-sm text-muted-foreground">{t('noLogs')}</p>
                ) : (
                  logs.map((entry, idx) => (
                    <div key={idx} className="space-y-1 rounded-md border border-border p-2.5 text-xs">
                      <div className="flex items-center justify-between gap-2 text-muted-foreground">
                        <Badge variant={entry.level === 'error' ? 'destructive' : 'outline'}>
                          {t.has(`logLevels.${entry.level}`) ? t(`logLevels.${entry.level}`) : entry.level}
                        </Badge>
                        <RelativeTime date={entry.time} />
                      </div>
                      <p className="min-w-0 break-all whitespace-pre-wrap font-mono text-foreground">{entry.message}</p>
                    </div>
                  ))
                )}
              </TabsContent>

              <TabsContent value="routes" className="space-y-2">
                {routes.length === 0 ? (
                  <p className="text-sm text-muted-foreground">{t('noRoutes')}</p>
                ) : (
                  routes.map((rt, idx) => (
                    <div key={idx} className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border px-3 py-2 text-xs">
                      <div className="flex min-w-0 flex-1 items-center gap-2">
                        <Badge variant="outline" className="shrink-0">{rt.method}</Badge>
                        <code className="min-w-0 break-all text-foreground">/api/plugins/{detail.id}{rt.pattern}</code>
                      </div>
                      <div className="flex shrink-0 items-center gap-1.5">
                        {rt.streamProxy ? (
                          <Badge variant="outline" className="border-primary/40 bg-primary/10 text-primary">
                            {t('streamProxyBadge')}
                          </Badge>
                        ) : null}
                        <Badge variant="secondary">
                          {t.has(`authLevels.${rt.auth}`) ? t(`authLevels.${rt.auth}`) : rt.auth}
                        </Badge>
                      </div>
                    </div>
                  ))
                )}
              </TabsContent>

              <TabsContent value="permissions" className="space-y-2">
                {permissions.length === 0 ? (
                  <p className="text-sm text-muted-foreground">{te('noPermissions')}</p>
                ) : (
                  permissions.map((perm) => {
                    const meta = permissionMeta.get(perm)
                    const permKey = `permissionDescriptions.${permissionMessageKey(perm)}`
                    const desc = te.has(permKey) ? te(permKey) : meta?.description
                    const tierLabel =
                      meta?.tier === 'high_risk'
                        ? ti('tierHighRisk')
                        : meta?.tier === 'sensitive'
                          ? ti('tierSensitive')
                          : meta?.tier === 'normal'
                            ? ti('tierNormal')
                            : null
                    const tierTone =
                      meta?.tier === 'high_risk'
                        ? 'border-destructive/40 bg-destructive/10 text-destructive'
                        : meta?.tier === 'sensitive'
                          ? 'border-warning/40 bg-warning/10 text-foreground'
                          : 'border-border text-muted-foreground'
                    return (
                      <div key={perm} className="space-y-1 rounded-md border border-border px-3 py-2 text-xs">
                        <div className="flex flex-wrap items-center justify-between gap-2">
                          <code className="min-w-0 break-all font-mono font-medium text-foreground">{perm}</code>
                          {tierLabel ? (
                            <Badge variant="outline" className={tierTone}>
                              {tierLabel}
                            </Badge>
                          ) : null}
                        </div>
                        {desc ? <p className="text-muted-foreground">{desc}</p> : null}
                      </div>
                    )
                  })
                )}
              </TabsContent>
            </Tabs>
          </div>
        ) : null}
      </SheetContent>
    </Sheet>
  )
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="min-w-0 break-all font-mono text-sm font-medium text-foreground tabular-nums">{value}</div>
    </div>
  )
}
