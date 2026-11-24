'use client'

import {
  type AdminPluginView,
  disablePlugin,
  enablePlugin,
  listPlugins,
  reloadPlugin,
  uninstallPlugin,
} from '@skinloom/api-client'
import {
  Alert,
  AlertAction,
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
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
  Checkbox,
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
  Field,
  FieldLabel,
  Input,
  PageHeader,
  Skeleton,
  Spinner,
  StatusDot,
  Switch,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  toast,
} from '@skinloom/ui'
import { Box, CircleAlert, Cpu, FileText, Puzzle, RefreshCw, Settings, ShieldAlert, Store, Trash2, Upload } from 'lucide-react'
import { useRouter } from 'next/navigation'
import { useLocale, useTranslations } from 'next-intl'
import { useCallback, useEffect, useRef, useState } from 'react'

import { problemOf } from '@/lib/problem'

import { PluginDetailsDrawer } from './plugins-details-drawer'
import { PluginInstallDialog } from './plugins-install-dialog'
import { type MarketCategory, PluginsMarket } from './plugins-market'
import { PluginSettingsDrawer } from './plugins-settings-drawer'
import { apiProblemMessage, permissionMessageKey } from './plugins-settings-form'

function isNativePlugin(plugin: AdminPluginView): boolean {
  return plugin.runtime === 'native' || plugin.grantedPermissions.includes('native.exec')
}

export function PluginsManager({
  superadmin,
  initialTab = 'installed',
  initialMarketCategory = 'all',
}: {
  superadmin: boolean
  initialTab?: 'installed' | 'market'
  initialMarketCategory?: MarketCategory
}) {
  const t = useTranslations('adminPlugins')
  const tc = useTranslations('common')
  const locale = useLocale()
  const router = useRouter()
  const loadErrRef = useRef(t('errors.load'))
  loadErrRef.current = t('errors.load')
  const [activeTab, setActiveTab] = useState<'installed' | 'market'>(initialTab)
  useEffect(() => {
    setActiveTab(initialTab)
  }, [initialTab])
  const [plugins, setPlugins] = useState<AdminPluginView[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [refreshing, setRefreshing] = useState(false)
  const [busyIds, setBusyIds] = useState<Set<string>>(() => new Set())
  const latestRef = useRef(0)
  const abortRef = useRef<AbortController | null>(null)

  const [installOpen, setInstallOpen] = useState(false)
  const [settingsTarget, setSettingsTarget] = useState<AdminPluginView | null>(null)
  const [detailsTarget, setDetailsTarget] = useState<AdminPluginView | null>(null)
  const [uninstallTarget, setUninstallTarget] = useState<AdminPluginView | null>(null)
  const [purgeData, setPurgeData] = useState(false)
  const [purgeConfirmId, setPurgeConfirmId] = useState('')

  const addBusy = (id: string) =>
    setBusyIds((prev) => {
      const next = new Set(prev)
      next.add(id)
      return next
    })
  const removeBusy = (id: string) =>
    setBusyIds((prev) => {
      if (!prev.has(id)) return prev
      const next = new Set(prev)
      next.delete(id)
      return next
    })

  const refresh = useCallback(async () => {
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    const seq = ++latestRef.current
    setRefreshing(true)
    setError(null)
    try {
      const res = await listPlugins({ signal: controller.signal })
      if (latestRef.current !== seq) return
      setPlugins((res.data as AdminPluginView[]) ?? [])
    } catch (err) {
      if (controller.signal.aborted || latestRef.current !== seq) return
      setError(apiProblemMessage(problemOf(err).detail, err, loadErrRef.current))
    } finally {
      if (latestRef.current === seq) setRefreshing(false)
    }
  }, [])

  useEffect(() => {
    void locale
    void refresh()
    return () => abortRef.current?.abort()
  }, [refresh, locale])

  function replacePlugin(updated: AdminPluginView) {
    setPlugins((prev) => {
      if (!prev) return [updated]
      const idx = prev.findIndex((p) => p.id === updated.id)
      if (idx === -1) return [...prev, updated].toSorted((a, b) => a.id.localeCompare(b.id))
      const copy = [...prev]
      copy[idx] = updated
      return copy
    })
  }

  async function handleToggle(plugin: AdminPluginView, enable: boolean) {
    if (isNativePlugin(plugin) && !superadmin) return
    addBusy(plugin.id)
    try {
      const res = enable ? await enablePlugin(plugin.id) : await disablePlugin(plugin.id)
      replacePlugin(res.data as AdminPluginView)
      toast.success(t(enable ? 'toasts.enabled' : 'toasts.disabled'))
      router.refresh()
    } catch (err) {
      toast.error(apiProblemMessage(problemOf(err).detail, err, t('errors.action')))
    } finally {
      removeBusy(plugin.id)
    }
  }

  async function handleReload(plugin: AdminPluginView) {
    if (isNativePlugin(plugin) && !superadmin) return
    addBusy(plugin.id)
    try {
      const res = await reloadPlugin(plugin.id)
      replacePlugin(res.data as AdminPluginView)
      toast.success(t('toasts.reloaded'))
      router.refresh()
    } catch (err) {
      toast.error(apiProblemMessage(problemOf(err).detail, err, t('errors.action')))
    } finally {
      removeBusy(plugin.id)
    }
  }

  async function handleUninstallConfirm() {
    if (!superadmin || !uninstallTarget) return
    if (purgeData && purgeConfirmId.trim() !== uninstallTarget.id) return
    const id = uninstallTarget.id
    addBusy(id)
    try {
      await uninstallPlugin(id, { purge: purgeData })
      setPlugins((prev) => prev?.filter((p) => p.id !== id) ?? [])
      toast.success(t('toasts.uninstalled', { id }))
      setUninstallTarget(null)
      setPurgeData(false)
      setPurgeConfirmId('')
      router.refresh()
    } catch (err) {
      toast.error(apiProblemMessage(problemOf(err).detail, err, t('errors.action')))
    } finally {
      removeBusy(id)
    }
  }

  const uninstalling = Boolean(uninstallTarget && busyIds.has(uninstallTarget.id))
  const hasPlugins = (plugins?.length ?? 0) > 0

  const activeSettingsTarget = settingsTarget
    ? (plugins?.find((p) => p.id === settingsTarget.id) ?? settingsTarget)
    : null
  const activeDetailsTarget = detailsTarget
    ? (plugins?.find((p) => p.id === detailsTarget.id) ?? detailsTarget)
    : null
  const activeUninstallTarget = uninstallTarget
    ? (plugins?.find((p) => p.id === uninstallTarget.id) ?? uninstallTarget)
    : null

  return (
    <div className="flex min-w-0 flex-col gap-6">
      <PageHeader
        title={t('title')}
        description={t('intro')}
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Button type="button" variant="outline" loading={refreshing} onClick={() => void refresh()}>
              <RefreshCw />
              {t('actions.refresh')}
            </Button>
            {superadmin && (hasPlugins || activeTab === 'market') ? (
              <Button type="button" onClick={() => setInstallOpen(true)}>
                <Upload />
                {t('uploadButton')}
              </Button>
            ) : null}
          </div>
        }
      />

      <Tabs
        value={activeTab}
        onValueChange={(val) => setActiveTab(val === 'market' ? 'market' : 'installed')}
        className="flex flex-col gap-6"
      >
        <TabsList variant="line" className="no-scrollbar h-auto w-full justify-start overflow-x-auto overflow-y-hidden pb-1.5">
          <TabsTrigger value="installed">
            <Puzzle className="size-4" aria-hidden />
            {t('tabs.installed')}
            {plugins !== null ? (
              <Badge variant="secondary" className="ml-1 tabular-nums">
                {plugins.length}
              </Badge>
            ) : null}
          </TabsTrigger>
          <TabsTrigger value="market">
            <Store className="size-4" aria-hidden />
            {t('tabs.market')}
          </TabsTrigger>
        </TabsList>

        <TabsContent value="installed" className="space-y-6">
          {error ? (
            <Alert variant="destructive" role="alert">
              <CircleAlert />
              <AlertDescription>{error}</AlertDescription>
              <AlertAction>
                <Button type="button" variant="outline" size="sm" onClick={() => void refresh()}>
                  {tc('retry')}
                </Button>
              </AlertAction>
            </Alert>
          ) : null}

          {plugins === null && !error ? (
            <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
              <Skeleton className="h-44 rounded-xl" />
              <Skeleton className="h-44 rounded-xl" />
            </div>
          ) : plugins === null ? null : plugins.length === 0 ? (
            <Empty className="border border-dashed border-border py-16">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <Puzzle />
                </EmptyMedia>
                <EmptyTitle>{t('emptyTitle')}</EmptyTitle>
                <EmptyDescription>{t('emptyDescription')}</EmptyDescription>
              </EmptyHeader>
              <EmptyContent className="flex flex-wrap items-center justify-center gap-2">
                <Button type="button" variant="outline" onClick={() => setActiveTab('market')}>
                  <Store aria-hidden />
                  {t('tabs.market')}
                </Button>
                {superadmin ? (
                  <Button type="button" onClick={() => setInstallOpen(true)}>
                    <Upload aria-hidden />
                    {t('uploadButton')}
                  </Button>
                ) : null}
              </EmptyContent>
            </Empty>
          ) : (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          {plugins.map((plugin) => {
            const busy = busyIds.has(plugin.id)
            const statusDot = plugin.status === 'enabled' ? 'ok' : plugin.status === 'failed' ? 'error' : 'off'
            const isNative = isNativePlugin(plugin)
            // Backend pluginRequiresSuperadmin guards enable/disable/reload/settings on native plugins.
            const nativeLocked = isNative && !superadmin
            return (
              <Card key={plugin.id} className="flex flex-col justify-between">
                <CardHeader className="gap-3">
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0 space-y-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <CardTitle className="min-w-0 max-w-full truncate">{plugin.name}</CardTitle>
                        <Badge variant="outline" className="h-auto min-w-0 max-w-full whitespace-normal break-all font-mono text-xs">
                          {plugin.id} v{plugin.version}
                        </Badge>
                        <Badge variant="secondary">
                          {t.has(`kinds.${plugin.kind}`) ? t(`kinds.${plugin.kind}`) : plugin.kind}
                        </Badge>
                        {plugin.kind === 'plugin' ? (
                          <Badge
                            variant="outline"
                            className={
                              isNative
                                ? 'gap-1 border-primary/40 bg-primary/10 text-primary'
                                : 'gap-1 text-muted-foreground'
                            }
                          >
                            {isNative ? <Cpu className="size-3" aria-hidden /> : <Box className="size-3" aria-hidden />}
                            {t(`runtimes.${isNative ? 'native' : 'js'}`)}
                          </Badge>
                        ) : null}
                        <Badge variant="outline">
                          {t.has(`sources.${plugin.source}`) ? t(`sources.${plugin.source}`) : plugin.source}
                        </Badge>
                      </div>
                      {plugin.description ? <CardDescription>{plugin.description}</CardDescription> : null}
                    </div>
                    <div className="flex shrink-0 items-center gap-2" title={nativeLocked ? t('nativeSuperadminOnly') : undefined}>
                      {busy ? <Spinner className="size-4" /> : null}
                      <Switch
                        aria-label={plugin.name}
                        checked={plugin.status === 'enabled'}
                        disabled={busy || nativeLocked}
                        onCheckedChange={(checked) => void handleToggle(plugin, Boolean(checked))}
                      />
                    </div>
                  </div>
                </CardHeader>

                <CardContent className="space-y-3">
                  <div className="flex flex-wrap items-center gap-3">
                    <StatusDot status={statusDot}>
                      {t.has(`statuses.${plugin.status}`) ? t(`statuses.${plugin.status}`) : plugin.status}
                    </StatusDot>
                    {nativeLocked ? (
                      <Badge variant="outline" className="text-muted-foreground">
                        {t('nativeSuperadminOnly')}
                      </Badge>
                    ) : null}
                    {isNative && plugin.nativeStats?.running && plugin.nativeStats.pid > 0 ? (
                      <Badge variant="outline" className="font-mono text-xs tabular-nums">
                        {t('nativeRunningPid', { pid: plugin.nativeStats.pid })}
                      </Badge>
                    ) : null}
                    {isNative && plugin.platforms && plugin.platforms.length > 0 ? (
                      <div className="flex min-w-0 flex-wrap gap-1">
                        {plugin.platforms.map((plat) => (
                          <Badge key={plat} variant="secondary" className="font-mono text-xs">
                            {plat}
                          </Badge>
                        ))}
                      </div>
                    ) : null}
                    {plugin.grantedPermissions.length === 0 ? (
                      <span className="text-xs text-muted-foreground">{t('noPermissions')}</span>
                    ) : (
                      <div className="flex min-w-0 flex-wrap gap-1">
                        {plugin.grantedPermissions.slice(0, 4).map((p) => {
                          const permKey = `permissionDescriptions.${permissionMessageKey(p)}`
                          const permDesc = t.has(permKey) ? t(permKey) : undefined
                          return (
                            <Badge
                              key={p}
                              variant="outline"
                              title={permDesc}
                              className="h-auto min-w-0 max-w-full whitespace-normal break-all font-mono text-xs"
                            >
                              {p}
                            </Badge>
                          )
                        })}
                        {plugin.grantedPermissions.length > 4 ? (
                          <Badge variant="outline">+{plugin.grantedPermissions.length - 4}</Badge>
                        ) : null}
                      </div>
                    )}
                  </div>

                  {plugin.circuitTripped || plugin.error ? (
                    <Alert variant="destructive">
                      <ShieldAlert />
                      <AlertTitle>
                        {plugin.circuitTripped
                          ? t('circuitAlert', { count: plugin.consecutiveErrors ?? 5 })
                          : t('statuses.failed')}
                      </AlertTitle>
                      {plugin.error ? <AlertDescription className="min-w-0 break-all line-clamp-2 font-mono text-xs">{plugin.error}</AlertDescription> : null}
                    </Alert>
                  ) : null}
                </CardContent>

                <CardFooter className="flex flex-wrap justify-between gap-2 border-t border-border pt-3">
                  <div className="flex flex-wrap gap-2">
                    {plugin.hasSettings ? (
                      <Button type="button" variant="outline" size="sm" className="pointer-coarse:h-10" onClick={() => setSettingsTarget(plugin)}>
                        <Settings className="size-4" aria-hidden />
                        {t('actions.configure')}
                      </Button>
                    ) : null}
                    <Button type="button" variant="outline" size="sm" className="pointer-coarse:h-10" onClick={() => setDetailsTarget(plugin)}>
                      <FileText className="size-4" aria-hidden />
                      {t('actions.details')}
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="pointer-coarse:h-10"
                      disabled={busy || nativeLocked}
                      title={nativeLocked ? t('nativeSuperadminOnly') : undefined}
                      onClick={() => void handleReload(plugin)}
                    >
                      <RefreshCw className="size-4" aria-hidden />
                      {t('actions.reload')}
                    </Button>
                  </div>
                  {superadmin ? (
                    <Button
                      type="button"
                      variant="destructive-soft"
                      size="sm"
                      className="pointer-coarse:h-10"
                      disabled={busy}
                      onClick={() => {
                        setPurgeData(false)
                        setPurgeConfirmId('')
                        setUninstallTarget(plugin)
                      }}
                    >
                      <Trash2 className="size-4" aria-hidden />
                      {t('actions.uninstall')}
                    </Button>
                  ) : null}
                </CardFooter>
              </Card>
            )
          })}
        </div>
      )}
        </TabsContent>

        <TabsContent value="market" className="min-w-0">
          <PluginsMarket
            superadmin={superadmin}
            initialCategory={initialMarketCategory}
            onInstalled={(installedList) => {
              for (const item of installedList) {
                replacePlugin(item)
              }
              void refresh()
              router.refresh()
            }}
          />
        </TabsContent>
      </Tabs>

      <PluginInstallDialog
        open={installOpen}
        superadmin={superadmin}
        onOpenChange={setInstallOpen}
        onInstalled={(installed) => {
          replacePlugin(installed)
          router.refresh()
        }}
      />

      <PluginSettingsDrawer
        pluginId={activeSettingsTarget?.id ?? null}
        pluginName={activeSettingsTarget?.name ?? ''}
        open={activeSettingsTarget !== null}
        readOnly={activeSettingsTarget ? isNativePlugin(activeSettingsTarget) && !superadmin : false}
        onOpenChange={(next) => {
          if (!next) setSettingsTarget(null)
        }}
        onSaved={() => {
          void refresh()
          router.refresh()
        }}
      />

      <PluginDetailsDrawer
        plugin={activeDetailsTarget}
        open={activeDetailsTarget !== null}
        onOpenChange={(next) => {
          if (!next) setDetailsTarget(null)
        }}
      />

      <AlertDialog
        open={activeUninstallTarget !== null}
        onOpenChange={(next) => {
          if (!next && !uninstalling) {
            setUninstallTarget(null)
            setPurgeData(false)
            setPurgeConfirmId('')
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('uninstallDialog.title', { name: activeUninstallTarget?.name ?? '' })}</AlertDialogTitle>
            <AlertDialogDescription>{t('uninstallDialog.description')}</AlertDialogDescription>
          </AlertDialogHeader>
          <div className="space-y-3">
            <label className="flex min-h-8 cursor-pointer items-start gap-2.5 rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-xs text-foreground pointer-coarse:min-h-10">
              <Checkbox
                checked={purgeData}
                disabled={uninstalling}
                onCheckedChange={(c) => {
                  const checked = Boolean(c)
                  setPurgeData(checked)
                  if (!checked) setPurgeConfirmId('')
                }}
              />
              <span>{t('uninstallDialog.purgeLabel')}</span>
            </label>
            {purgeData && activeUninstallTarget ? (
              <Field>
                <FieldLabel htmlFor="uninstall-confirm-id">
                  {t('uninstallDialog.confirmIdLabel', { id: activeUninstallTarget.id })}
                </FieldLabel>
                <Input
                  id="uninstall-confirm-id"
                  value={purgeConfirmId}
                  disabled={uninstalling}
                  onChange={(e) => setPurgeConfirmId(e.target.value)}
                  placeholder={activeUninstallTarget.id}
                  autoComplete="off"
                  className="font-mono"
                />
              </Field>
            ) : null}
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={uninstalling}>{t('uninstallDialog.cancel')}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              loading={uninstalling}
              disabled={
                uninstalling ||
                (purgeData && activeUninstallTarget !== null && purgeConfirmId.trim() !== activeUninstallTarget.id)
              }
              onClick={() => void handleUninstallConfirm()}
            >
              {t('uninstallDialog.confirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
