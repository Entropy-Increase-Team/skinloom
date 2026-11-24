'use client'

import {
  type AdminMarketInstallResult,
  type AdminPluginView,
  inspectMarketPackage,
  installMarketPackage,
  listMarketPackages,
  type MarketInspectResult,
  type MarketListResult,
  type MarketPackageView,
  type MarketRefreshResult,
  refreshMarketPackages,
} from '@skinloom/api-client'
import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
  cn,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerFooter,
  DrawerHeader,
  DrawerTitle,
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  Skeleton,
  Spinner,
  Switch,
  toast,
  useIsMobile,
} from '@skinloom/ui'
import {
  ArrowUpCircle,
  BadgeCheck,
  Box,
  CheckCircle2,
  CircleAlert,
  Cpu,
  Download,
  Globe,
  History,
  Laptop,
  Monitor,
  RefreshCw,
  Search,
  Server,
  ShieldAlert,
  Store,
} from 'lucide-react'
import { useLocale, useTranslations } from 'next-intl'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { problemOf } from '@/lib/problem'

import {
  PackageInspectHeader,
  PackagePermissionsPanel,
  PackageTrustAndRiskAlerts,
  TypedConfirmPluginId,
} from './plugins-package-inspect'
import {
  apiProblemMessage,
  canSubmitPackageInstall,
  collectInitialGrantedPermissions,
  permissionMessageKey,
} from './plugins-settings-form'

const CATEGORIES = [
  'all',
  'recommended',
  'sandbox',
  'native',
  'theme',
  'security',
  'score',
  'utility',
  'migration',
] as const

export type MarketCategory = (typeof CATEGORIES)[number]

export function summarizePlatformFamilies(platforms: string[] | undefined): ('any' | 'linux' | 'windows' | 'macos')[] {
  if (!platforms || platforms.length === 0) return ['any']
  const set = new Set<'any' | 'linux' | 'windows' | 'macos'>()
  for (const p of platforms) {
    const lower = p.toLowerCase()
    if (lower === 'any') set.add('any')
    else if (lower.startsWith('linux/')) set.add('linux')
    else if (lower.startsWith('windows/')) set.add('windows')
    else if (lower.startsWith('darwin/')) set.add('macos')
  }
  return set.size > 0 ? Array.from(set) : ['any']
}

export function PluginsMarket({
  superadmin,
  initialCategory = 'all',
  onInstalled,
}: {
  superadmin: boolean
  initialCategory?: MarketCategory
  onInstalled: (installed: AdminPluginView[]) => void
}) {
  const t = useTranslations('adminPlugins')
  const tm = useTranslations('adminPlugins.market')
  const tc = useTranslations('common')
  const locale = useLocale()
  const loadErrRef = useRef(t('errors.load'))
  loadErrRef.current = t('errors.load')

  const [market, setMarket] = useState<MarketListResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const marketSeqRef = useRef(0)
  const marketAbortRef = useRef<AbortController | null>(null)

  const [compatibleOnly, setCompatibleOnly] = useState(true)
  const [category, setCategory] = useState<MarketCategory>(initialCategory)
  const [query, setQuery] = useState('')

  const [installTarget, setInstallTarget] = useState<{
    pkg: MarketPackageView
    version?: string
  } | null>(null)
  const [changelogPkg, setChangelogPkg] = useState<MarketPackageView | null>(null)

  useEffect(() => {
    setCategory(initialCategory)
  }, [initialCategory])

  const loadMarket = useCallback(async () => {
    marketAbortRef.current?.abort()
    const ctl = new AbortController()
    marketAbortRef.current = ctl
    const seq = ++marketSeqRef.current
    setLoading(true)
    setError(null)
    try {
      const res = await listMarketPackages({ compatibleOnly: false }, { signal: ctl.signal })
      if (seq !== marketSeqRef.current) return
      setMarket(res.data as MarketListResult)
    } catch (err) {
      if (ctl.signal.aborted || seq !== marketSeqRef.current) return
      setError(apiProblemMessage(problemOf(err).detail, err, loadErrRef.current))
    } finally {
      if (seq === marketSeqRef.current) setLoading(false)
    }
  }, [])

  useEffect(() => {
    void locale
    void loadMarket()
    return () => marketAbortRef.current?.abort()
  }, [loadMarket, locale])

  async function handleForceRefresh() {
    setRefreshing(true)
    setError(null)
    try {
      const res = await refreshMarketPackages()
      const data = res.data as MarketRefreshResult
      setMarket({
        hostEnv: data.hostEnv,
        registries: data.registries,
        packages: data.packages,
      })
      toast.success(tm('refreshedToast'))
      if (data.blockedActions && data.blockedActions.length > 0) {
        toast.error(tm('blockedDisabledToast', { count: data.blockedActions.length }))
        onInstalled([])
      }
    } catch (err) {
      toast.error(apiProblemMessage(problemOf(err).detail, err, t('errors.action')))
    } finally {
      setRefreshing(false)
    }
  }

  const filteredPackages = useMemo(() => {
    if (!market) return []
    const q = query.trim().toLowerCase()
    return market.packages.filter((pkg) => {
      if (compatibleOnly && !pkg.compatibility.compatible) return false
      if (category !== 'all') {
        if (category === 'recommended' && !pkg.official) return false
        if (category === 'sandbox' && (pkg.runtime !== 'js' || pkg.kind !== 'plugin')) return false
        if (category === 'native' && pkg.runtime !== 'native') return false
        if (category === 'theme' && pkg.kind !== 'theme') return false
        if (category === 'security' && pkg.category !== 'security' && pkg.category !== 'auth') return false
        if (category === 'score' && pkg.category !== 'score' && pkg.category !== 'economy' && pkg.category !== 'operations') return false
        if (
          category === 'utility' &&
          pkg.category !== 'utility' &&
          pkg.category !== 'storage' &&
          pkg.category !== 'render' &&
          pkg.category !== 'search'
        ) {
          return false
        }
        if (category === 'migration' && pkg.category !== 'migration') return false
      }
      if (q) {
        const hay = [
          pkg.id,
          pkg.name,
          pkg.description ?? '',
          pkg.category ?? '',
          ...(pkg.authors ?? []),
        ]
          .join(' ')
          .toLowerCase()
        if (!hay.includes(q)) return false
      }
      return true
    })
  }, [market, compatibleOnly, category, query])

  const failedRegistries = useMemo(
    () => (market?.registries ?? []).filter((r) => r.enabled && Boolean(r.error)),
    [market],
  )

  return (
    <div className="flex min-w-0 flex-col gap-5">
      {/* Server environment capsule, compatibleOnly toggle, and refresh button */}
      <div className="flex min-w-0 flex-wrap items-center justify-between gap-3 rounded-xl border border-border bg-card p-3.5">
        <div className="flex min-w-0 max-w-full flex-wrap items-center gap-2.5">
          {market?.hostEnv ? (
            <Badge
              variant="secondary"
              className="h-auto max-w-full gap-1.5 whitespace-normal break-words px-2.5 py-1 font-mono text-xs"
            >
              <Server className="size-3.5 shrink-0 text-primary" />
              <span className="min-w-0 break-all">
                {tm('hostCapsule', {
                  platform: market.hostEnv.platform,
                  skinloom: market.hostEnv.skinloomVersion,
                  go: market.hostEnv.goVersion,
                })}
              </span>
            </Badge>
          ) : (
            <Skeleton className="h-6 w-56 max-w-full rounded-md" />
          )}
        </div>

        <div className="flex flex-wrap items-center gap-3">
          <label
            htmlFor="market-compatible-only"
            className="flex min-h-8 cursor-pointer items-center gap-2 text-sm text-foreground pointer-coarse:min-h-10"
          >
            <Switch
              id="market-compatible-only"
              aria-label={tm('compatibleOnly')}
              checked={compatibleOnly}
              onCheckedChange={(c) => setCompatibleOnly(Boolean(c))}
            />
            <span>{tm('compatibleOnly')}</span>
          </label>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="pointer-coarse:h-10"
            loading={refreshing}
            onClick={() => void handleForceRefresh()}
          >
            <RefreshCw />
            {tm('refreshIndex')}
          </Button>
        </div>
      </div>

      {/* Category pills and search box */}
      <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
        <div role="group" aria-label={tm('categories.all')} className="no-scrollbar flex min-w-0 max-w-full items-center gap-1.5 overflow-x-auto pb-1 sm:flex-wrap sm:overflow-visible sm:pb-0">
          {CATEGORIES.map((cat) => (
            <Button
              key={cat}
              type="button"
              size="sm"
              aria-pressed={category === cat}
              variant={category === cat ? 'secondary' : 'outline'}
              className={cn(
                'shrink-0 pointer-coarse:h-10',
                category === cat && 'border border-primary/40 bg-primary/10 text-primary hover:bg-primary/15',
              )}
              onClick={() => setCategory(cat)}
            >
              {tm(`categories.${cat}`)}
            </Button>
          ))}
        </div>

        <InputGroup className="w-full sm:w-72">
          <InputGroupAddon>
            <Search aria-hidden />
          </InputGroupAddon>
          <InputGroupInput
            type="search"
            value={query}
            placeholder={tm('searchPlaceholder')}
            aria-label={tm('searchPlaceholder')}
            onChange={(e) => setQuery(e.target.value)}
          />
        </InputGroup>
      </div>

      {error ? (
        <Alert variant="destructive" role="alert">
          <CircleAlert />
          <AlertDescription>{error}</AlertDescription>
          <AlertAction>
            <Button type="button" variant="outline" size="sm" onClick={() => void loadMarket()}>
              {tc('retry')}
            </Button>
          </AlertAction>
        </Alert>
      ) : null}

      {failedRegistries.map((reg) => (
        <Alert key={reg.id} variant="destructive">
          <ShieldAlert />
          <AlertTitle>
            {tm('registryErrorTitle', {
              name: reg.official && (!reg.name || reg.name === 'SkinLoom Official Registry')
                ? tm('officialRegistryName')
                : reg.name || reg.id,
            })}
          </AlertTitle>
          <AlertDescription className="font-mono text-xs break-all">{reg.error}</AlertDescription>
        </Alert>
      ))}

      {loading && !market ? (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <Skeleton className="h-48 rounded-xl" />
          <Skeleton className="h-48 rounded-xl" />
        </div>
      ) : !market ? null : filteredPackages.length === 0 ? (
        <Empty className="border border-dashed border-border py-16">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Store />
            </EmptyMedia>
            <EmptyTitle>{tm('emptyTitle')}</EmptyTitle>
            <EmptyDescription>{tm('emptyDescription')}</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          {filteredPackages.map((pkg) => {
            const isNative = pkg.runtime === 'native'
            const isCompat = pkg.compatibility.compatible
            const recVer = pkg.compatibility.recommendedVersion || pkg.latest
            const families = summarizePlatformFamilies(pkg.supportedPlatforms)
            const firstIssue = pkg.compatibility.issues?.[0]?.message || pkg.compatibility.status
            const allPerms = [
              ...(pkg.permissions.normal ?? []),
              ...(pkg.permissions.sensitive ?? []),
              ...(pkg.permissions.high_risk ?? []),
            ]

            return (
              <Card
                key={`${pkg.registryId}:${pkg.id}`}
                className={`flex flex-col justify-between ${!isCompat ? 'opacity-75' : ''}`}
              >
                <CardHeader className="gap-3">
                  <div className="flex flex-wrap items-start justify-between gap-2 sm:flex-nowrap sm:gap-3">
                    <div className="min-w-0 flex-1 space-y-1.5">
                      <div className="flex flex-wrap items-center gap-2">
                        <CardTitle className="min-w-0 max-w-full truncate">{pkg.name}</CardTitle>
                        <Badge variant="outline" className="h-auto min-w-0 max-w-full whitespace-normal break-all font-mono text-xs">
                          {pkg.id} v{recVer}
                        </Badge>
                        <Badge variant="secondary">
                          {t.has(`kinds.${pkg.kind}`) ? t(`kinds.${pkg.kind}`) : pkg.kind}
                        </Badge>
                        {pkg.kind === 'plugin' ? (
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
                        {pkg.official ? (
                          <Badge variant="outline" className="gap-1 border-success/40 bg-success/10 text-success">
                            <BadgeCheck className="size-3" aria-hidden />
                            {tm('officialBadge')}
                          </Badge>
                        ) : null}
                      </div>
                      {pkg.description ? <CardDescription>{pkg.description}</CardDescription> : null}
                    </div>

                    {pkg.installed ? (
                      pkg.upgradeAvailable ? (
                        <Badge className="shrink-0 gap-1">
                          <ArrowUpCircle className="size-3" aria-hidden />
                          {tm('upgradeBadge', { version: recVer })}
                        </Badge>
                      ) : (
                        <Badge variant="secondary" className="shrink-0 gap-1">
                          <CheckCircle2 className="size-3" aria-hidden />
                          {tm('installedBadge', { version: pkg.installedVersion || recVer })}
                        </Badge>
                      )
                    ) : null}
                  </div>
                </CardHeader>

                <CardContent className="space-y-3">
                  {/* Platform badges & compatibility status */}
                  <div className="flex flex-wrap items-center gap-2">
                    {families.map((fam) => (
                      <Badge key={fam} variant="secondary" className="gap-1 text-xs">
                        {fam === 'any' ? (
                          <Globe className="size-3" aria-hidden />
                        ) : fam === 'linux' ? (
                          <Server className="size-3" aria-hidden />
                        ) : fam === 'windows' ? (
                          <Monitor className="size-3" aria-hidden />
                        ) : (
                          <Laptop className="size-3" aria-hidden />
                        )}
                        {fam === 'any'
                          ? tm('platformAny')
                          : fam === 'linux'
                            ? tm('platformLinux')
                            : fam === 'windows'
                              ? tm('platformWindows')
                              : tm('platformMacOS')}
                      </Badge>
                    ))}
                    {isNative && pkg.supportedPlatforms && pkg.supportedPlatforms.length > 0 ? (
                      <div className="flex flex-wrap gap-1">
                        {pkg.supportedPlatforms.map((plat) => (
                          <Badge key={plat} variant="outline" className="font-mono text-xs">
                            {plat}
                          </Badge>
                        ))}
                      </div>
                    ) : null}
                  </div>

                  <div className="flex flex-wrap items-center gap-2 text-xs">
                    {isCompat ? (
                      <span className="inline-flex items-center gap-1 font-medium text-success">
                        <CheckCircle2 className="size-3.5 shrink-0" aria-hidden />
                        {tm('compatOk', { version: recVer })}
                      </span>
                    ) : (
                      <span className="inline-flex items-center gap-1 font-medium text-destructive">
                        <ShieldAlert className="size-3.5 shrink-0" aria-hidden />
                        {tm('compatIncompatible', { reason: firstIssue })}
                      </span>
                    )}
                    {pkg.authors && pkg.authors.length > 0 ? (
                      <span className="text-muted-foreground">· {pkg.authors.join(', ')}</span>
                    ) : null}
                  </div>

                  {allPerms.length > 0 ? (
                    <div className="flex flex-wrap gap-1">
                      {allPerms.slice(0, 4).map((p) => {
                        const permKey = `permissionDescriptions.${permissionMessageKey(p.name)}`
                        const permDesc = t.has(permKey) ? t(permKey) : p.description
                        return (
                          <Badge
                            key={p.name}
                            variant="outline"
                            title={permDesc}
                            className="h-auto min-w-0 max-w-full whitespace-normal break-all font-mono text-xs"
                          >
                            {p.name}
                          </Badge>
                        )
                      })}
                      {allPerms.length > 4 ? (
                        <Badge variant="outline" className="text-xs">
                          +{allPerms.length - 4}
                        </Badge>
                      ) : null}
                    </div>
                  ) : null}
                </CardContent>

                <CardFooter className="flex flex-wrap items-center justify-between gap-2 border-t border-border pt-3">
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    className="pointer-coarse:h-10"
                    onClick={() => setChangelogPkg(pkg)}
                  >
                    <History aria-hidden />
                    {tm('changelogButton')}
                  </Button>

                  {superadmin ? (
                    <Button
                      type="button"
                      size="sm"
                      className="pointer-coarse:h-10"
                      variant="outline"
                      disabled={!isCompat}
                      onClick={() => setInstallTarget({ pkg, version: recVer })}
                    >
                      <Download aria-hidden />
                      {pkg.upgradeAvailable
                        ? tm('upgradeButton')
                        : pkg.installed
                          ? tm('reinstallButton')
                          : tm('installButton')}
                    </Button>
                  ) : null}
                </CardFooter>
              </Card>
            )
          })}
        </div>
      )}

      <MarketInstallDialog
        target={installTarget}
        superadmin={superadmin}
        onOpenChange={(open) => {
          if (!open) setInstallTarget(null)
        }}
        onInstalled={(installed) => {
          onInstalled(installed)
          void loadMarket()
        }}
      />

      <MarketChangelogDrawer
        pkg={
          changelogPkg
            ? (market?.packages.find((p) => p.registryId === changelogPkg.registryId && p.id === changelogPkg.id) ??
                changelogPkg)
            : null
        }
        superadmin={superadmin}
        onOpenChange={(open) => {
          if (!open) setChangelogPkg(null)
        }}
        onSelectInstall={(pkg, version) => {
          setChangelogPkg(null)
          setInstallTarget({ pkg, version })
        }}
      />
    </div>
  )
}

function MarketChangelogDrawer({
  pkg,
  superadmin,
  onOpenChange,
  onSelectInstall,
}: {
  pkg: MarketPackageView | null
  superadmin: boolean
  onOpenChange: (open: boolean) => void
  onSelectInstall: (pkg: MarketPackageView, version: string) => void
}) {
  const tm = useTranslations('adminPlugins.market')
  const isPhone = useIsMobile()

  const versionsList = pkg ? (
    <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-4 pt-3 pb-[max(1.5rem,env(safe-area-inset-bottom))]">
      {pkg.versions.map((ver) => (
        <div key={ver.version} className="space-y-2 rounded-xl border border-border bg-card p-3.5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <span className="font-mono text-sm font-medium text-foreground">v{ver.version}</span>
              {ver.yanked ? <Badge variant="destructive">{tm('yankedBadge')}</Badge> : null}
              {ver.compatible ? (
                <Badge variant="outline" className="border-success/40 bg-success/10 text-success">
                  {tm('compatOk', { version: ver.version })}
                </Badge>
              ) : (
                <Badge
                  variant="outline"
                  className="h-auto max-w-full whitespace-normal break-words border-destructive/40 bg-destructive/10 text-left text-destructive"
                >
                  {ver.issues?.[0]?.message || ver.status}
                </Badge>
              )}
            </div>
            {superadmin && ver.compatible && !ver.yanked ? (
              <Button
                type="button"
                size="sm"
                variant="outline"
                className="pointer-coarse:h-10"
                onClick={() => onSelectInstall(pkg, ver.version)}
              >
                <Download />
                {tm('installVersion', { version: ver.version })}
              </Button>
            ) : null}
          </div>

          {ver.platforms && ver.platforms.length > 0 ? (
            <div className="flex flex-wrap gap-1">
              {ver.platforms.map((p) => (
                <Badge key={p} variant="secondary" className="font-mono text-xs">
                  {p}
                </Badge>
              ))}
            </div>
          ) : null}

          <p className="whitespace-pre-wrap [overflow-wrap:anywhere] text-xs text-muted-foreground">
            {ver.changelog || tm('noChangelog')}
          </p>
        </div>
      ))}
    </div>
  ) : null

  if (isPhone) {
    return (
      <Drawer open={Boolean(pkg)} onOpenChange={onOpenChange}>
        <DrawerContent>
          <DrawerHeader>
            <DrawerTitle>{tm('changelogDrawerTitle', { name: pkg?.name ?? '' })}</DrawerTitle>
            <DrawerDescription>{tm('changelogDrawerDescription')}</DrawerDescription>
          </DrawerHeader>
          {versionsList}
        </DrawerContent>
      </Drawer>
    )
  }

  return (
    <Sheet open={Boolean(pkg)} onOpenChange={onOpenChange}>
      <SheetContent className="flex w-full flex-col overflow-hidden data-[side=right]:w-full sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>{tm('changelogDrawerTitle', { name: pkg?.name ?? '' })}</SheetTitle>
          <SheetDescription>{tm('changelogDrawerDescription')}</SheetDescription>
        </SheetHeader>
        {versionsList}
      </SheetContent>
    </Sheet>
  )
}

function MarketInstallDialog({
  target,
  superadmin,
  onOpenChange,
  onInstalled,
}: {
  target: { pkg: MarketPackageView; version?: string } | null
  superadmin: boolean
  onOpenChange: (open: boolean) => void
  onInstalled: (installed: AdminPluginView[]) => void
}) {
  const t = useTranslations('adminPlugins.inspect')
  const te = useTranslations('adminPlugins')
  const tm = useTranslations('adminPlugins.market')
  const locale = useLocale()
  const inspectErrRef = useRef(te('errors.inspect'))
  inspectErrRef.current = te('errors.inspect')
  const isPhone = useIsMobile()

  const [inspecting, setInspecting] = useState(false)
  const [installing, setInstalling] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [inspect, setInspect] = useState<MarketInspectResult | null>(null)
  const [granted, setGranted] = useState<string[]>([])
  const [allowUnsigned, setAllowUnsigned] = useState(false)
  const [confirmId, setConfirmId] = useState('')
  const [retryNonce, setRetryNonce] = useState(0)
  const seqRef = useRef(0)
  const errorBoxRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    if (!target) {
      seqRef.current += 1
      setInspect(null)
      setInspecting(false)
      setError(null)
      setGranted([])
      setAllowUnsigned(false)
      setConfirmId('')
      return
    }
    const seq = ++seqRef.current
    const ctl = new AbortController()
    setInspecting(true)
    setError(null)
    setInspect(null)
    setAllowUnsigned(false)
    setConfirmId('')

    void locale
    inspectMarketPackage(
      {
        registryId: target.pkg.registryId,
        id: target.pkg.id,
        version: target.version,
      },
      { signal: ctl.signal },
    )
      .then((res) => {
        if (seq !== seqRef.current || ctl.signal.aborted) return
        const data = res.data as MarketInspectResult
        setInspect(data)
        const initGranted = collectInitialGrantedPermissions(data.permissions)
        if (data.runtime === 'native' && !initGranted.includes('native.exec')) {
          initGranted.push('native.exec')
        }
        setGranted(initGranted)
      })
      .catch((err) => {
        if (seq !== seqRef.current || ctl.signal.aborted || (err instanceof DOMException && err.name === 'AbortError')) {
          return
        }
        setError(apiProblemMessage(problemOf(err).detail, err, inspectErrRef.current))
      })
      .finally(() => {
        if (seq === seqRef.current && !ctl.signal.aborted) setInspecting(false)
      })
    return () => {
      ctl.abort()
    }
  }, [target, locale, retryNonce])

  async function handleConfirmInstall() {
    if (!target || !inspect) return
    setInstalling(true)
    setError(null)
    try {
      const res = await installMarketPackage({
        registryId: inspect.registryId || target.pkg.registryId,
        id: inspect.id,
        version: inspect.version,
        grantedPermissions: granted,
        allowUnsigned,
        installDependencies: true,
      })
      const data = res.data as AdminMarketInstallResult
      const allInstalled = [...(data.installedDependencies ?? []), data.plugin]
      toast.success(te('toasts.installed', { id: data.plugin.id, version: data.plugin.version }))
      onInstalled(allInstalled)
      onOpenChange(false)
    } catch (err) {
      const msg = apiProblemMessage(problemOf(err).detail, err, te('errors.install'))
      setError(msg)
      toast.error(msg)
      requestAnimationFrame(() => errorBoxRef.current?.scrollIntoView({ behavior: 'smooth', block: 'nearest' }))
    } finally {
      setInstalling(false)
    }
  }

  const togglePermission = (name: string, enabled: boolean) => {
    if (inspect?.runtime === 'native' && name === 'native.exec') return
    setGranted((prev) => (enabled ? (prev.includes(name) ? prev : [...prev, name]) : prev.filter((p) => p !== name)))
  }

  const isNative = inspect?.runtime === 'native'
  const nativeBlocked = Boolean(inspect?.nativeInstallBlocked)
  const platformIncompatible = Boolean(isNative && inspect && !inspect.platformCompatible)
  const hasHighRiskDep = Boolean(
    inspect?.dependenciesToInstall?.some((d) => d.requiresHighRiskConfirm || d.runtime === 'native'),
  )
  const hasHighRiskGranted =
    isNative ||
    hasHighRiskDep ||
    (inspect?.permissions.high_risk.some((p) => granted.includes(p.name)) ?? false)
  const needsTypedId = Boolean(
    inspect &&
      !nativeBlocked &&
      !platformIncompatible &&
      (!inspect.verification.trusted || ((inspect.requiresHighRiskConfirm || isNative || hasHighRiskDep) && hasHighRiskGranted)),
  )
  const canInstall =
    superadmin &&
    canSubmitPackageInstall(inspect, granted, allowUnsigned, confirmId) &&
    (!needsTypedId || confirmId.trim() === inspect?.id)

  const body = (
    <div className="space-y-4">
      <div ref={errorBoxRef}>
        <FormError message={error} />
      </div>

      {inspecting ? (
        <div className="flex flex-col items-center justify-center gap-2 py-8 text-center text-sm text-muted-foreground">
          <Spinner />
          <span>{tm('downloading')}</span>
        </div>
      ) : inspect ? (
        <div className="space-y-4">
          <PackageInspectHeader inspect={inspect} />

          {inspect.dependenciesToInstall && inspect.dependenciesToInstall.length > 0 ? (
            <div className="space-y-2 rounded-lg border border-primary/30 bg-primary/5 p-3">
              <p className="text-xs font-medium text-foreground">{tm('cascadeTitle')}</p>
              <p className="text-xs text-muted-foreground">{tm('cascadeHint')}</p>
              <div className="flex flex-wrap gap-1.5 pt-1">
                {inspect.dependenciesToInstall.map((dep) => (
                  <Badge
                    key={dep.id}
                    variant="secondary"
                    className="h-auto min-w-0 max-w-full gap-1 whitespace-normal break-all font-mono text-xs"
                  >
                    {dep.runtime === 'native' ? <Cpu className="size-3 shrink-0 text-primary" aria-hidden /> : <Box className="size-3 shrink-0" aria-hidden />}
                    <span>
                      {dep.name} ({dep.id} v{dep.version})
                    </span>
                  </Badge>
                ))}
              </div>
            </div>
          ) : null}

          <PackageTrustAndRiskAlerts
            inspect={inspect}
            allowUnsigned={allowUnsigned}
            disabled={installing}
            onAllowUnsignedChange={setAllowUnsigned}
          />

          <PackagePermissionsPanel
            inspect={inspect}
            granted={granted}
            disabled={installing}
            onToggle={togglePermission}
          />

          {needsTypedId ? (
            <TypedConfirmPluginId
              id={inspect.id}
              inputId="market-confirm-plugin-id"
              value={confirmId}
              disabled={installing}
              onChange={setConfirmId}
            />
          ) : null}
        </div>
      ) : null}
    </div>
  )

  const inspectFailed = Boolean(!inspecting && !inspect && error)
  const footer = (
    <>
      <Button
        type="button"
        variant="outline"
        className="pointer-coarse:h-10"
        onClick={() => onOpenChange(false)}
        disabled={installing}
      >
        {t('cancel')}
      </Button>
      {inspectFailed ? (
        <Button
          type="button"
          className="pointer-coarse:h-10"
          onClick={() => setRetryNonce((n) => n + 1)}
        >
          <RefreshCw />
          {tm('retryInspect')}
        </Button>
      ) : (
        <Button
          type="button"
          className="pointer-coarse:h-10"
          loading={installing}
          onClick={() => void handleConfirmInstall()}
          disabled={!canInstall || inspecting}
        >
          {t('confirmInstall')}
        </Button>
      )}
    </>
  )

  const open = Boolean(target)
  if (isPhone) {
    return (
      <Drawer open={open} onOpenChange={(next) => !installing && onOpenChange(next)}>
        <DrawerContent>
          <DrawerHeader>
            <DrawerTitle>{t('title')}</DrawerTitle>
            <DrawerDescription>{target?.pkg.name}</DrawerDescription>
          </DrawerHeader>
          <div className="min-h-0 flex-1 overflow-y-auto px-4 pt-3 pb-4">{body}</div>
          <DrawerFooter className="border-t pt-3 pb-[max(1rem,env(safe-area-inset-bottom))]">{footer}</DrawerFooter>
        </DrawerContent>
      </Drawer>
    )
  }

  return (
    <Dialog open={open} onOpenChange={(next) => !installing && onOpenChange(next)}>
      <DialogContent showCloseButton={!installing} className="flex max-h-[90dvh] flex-col overflow-hidden sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t('title')}</DialogTitle>
          <DialogDescription>{target?.pkg.name}</DialogDescription>
        </DialogHeader>
        <div className="min-h-0 flex-1 overflow-y-auto pr-1">{body}</div>
        <DialogFooter>{footer}</DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
