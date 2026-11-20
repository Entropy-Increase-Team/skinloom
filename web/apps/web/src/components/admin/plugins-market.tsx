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
  type PermissionItem,
  refreshMarketPackages,
} from '@skinloom/api-client'
import {
  Alert,
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
  Checkbox,
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
  Input,
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  Label,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  Skeleton,
  Spinner,
  Switch,
  toast,
} from '@skinloom/ui'
import {
  ArrowUpCircle,
  BadgeCheck,
  Box,
  CheckCircle2,
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
  ShieldCheck,
  ShieldQuestion,
  Store,
} from 'lucide-react'
import { useLocale, useTranslations } from 'next-intl'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { problemOf } from '@/lib/problem'

import {
  apiProblemMessage,
  canSubmitPackageInstall,
  collectInitialGrantedPermissions,
  formatBytes,
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

function useIsPhoneViewport() {
  const [mobile, setMobile] = useState(false)
  useEffect(() => {
    const mql = window.matchMedia('(max-width: 767px)')
    const update = () => setMobile(mql.matches)
    update()
    mql.addEventListener('change', update)
    return () => mql.removeEventListener('change', update)
  }, [])
  return mobile
}

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
  const locale = useLocale()
  const loadErrRef = useRef(t('errors.load'))
  loadErrRef.current = t('errors.load')

  const [market, setMarket] = useState<MarketListResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState<string | null>(null)

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
    setLoading(true)
    setError(null)
    try {
      const res = await listMarketPackages({ compatibleOnly: false })
      setMarket(res.data as MarketListResult)
    } catch (err) {
      setError(apiProblemMessage(problemOf(err).detail, err, loadErrRef.current))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void locale
    void loadMarket()
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
    <div className="flex flex-col gap-5">
      {/* Server environment capsule, compatibleOnly toggle, and refresh button */}
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-border bg-card p-3.5">
        <div className="flex flex-wrap items-center gap-2.5">
          {market?.hostEnv ? (
            <Badge variant="secondary" className="gap-1.5 px-2.5 py-1 font-mono text-xs">
              <Server className="size-3.5 text-primary" />
              {tm('hostCapsule', {
                platform: market.hostEnv.platform,
                skinloom: market.hostEnv.skinloomVersion,
                go: market.hostEnv.goVersion,
              })}
            </Badge>
          ) : (
            <Skeleton className="h-6 w-56 rounded-md" />
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
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="no-scrollbar flex max-w-full items-center gap-1.5 overflow-x-auto pb-1 sm:flex-wrap sm:overflow-visible sm:pb-0">
          {CATEGORIES.map((cat) => (
            <Button
              key={cat}
              type="button"
              size="sm"
              variant={category === cat ? 'default' : 'outline'}
              className="shrink-0 pointer-coarse:h-10"
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
            onChange={(e) => setQuery(e.target.value)}
          />
        </InputGroup>
      </div>

      <FormError message={error} />

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
      ) : filteredPackages.length === 0 ? (
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
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0 space-y-1.5">
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
                            {isNative ? <Cpu className="size-3" /> : <Box className="size-3" />}
                            {t(`runtimes.${isNative ? 'native' : 'js'}`)}
                          </Badge>
                        ) : null}
                        {pkg.official ? (
                          <Badge variant="outline" className="gap-1 border-success/40 bg-success/10 text-success">
                            <BadgeCheck className="size-3" />
                            {tm('officialBadge')}
                          </Badge>
                        ) : null}
                      </div>
                      {pkg.description ? <CardDescription>{pkg.description}</CardDescription> : null}
                    </div>

                    {pkg.installed ? (
                      pkg.upgradeAvailable ? (
                        <Badge className="shrink-0 gap-1">
                          <ArrowUpCircle className="size-3" />
                          {tm('upgradeBadge', { version: recVer })}
                        </Badge>
                      ) : (
                        <Badge variant="secondary" className="shrink-0 gap-1">
                          <CheckCircle2 className="size-3" />
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
                          <Globe className="size-3" />
                        ) : fam === 'linux' ? (
                          <Server className="size-3" />
                        ) : fam === 'windows' ? (
                          <Monitor className="size-3" />
                        ) : (
                          <Laptop className="size-3" />
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
                        <CheckCircle2 className="size-3.5" />
                        {tm('compatOk', { version: recVer })}
                      </span>
                    ) : (
                      <span className="inline-flex items-center gap-1 font-medium text-destructive">
                        <ShieldAlert className="size-3.5" />
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
                    variant="outline"
                    size="sm"
                    className="pointer-coarse:h-10"
                    onClick={() => setChangelogPkg(pkg)}
                  >
                    <History />
                    {tm('changelogButton')}
                  </Button>

                  {superadmin ? (
                    <Button
                      type="button"
                      size="sm"
                      className="pointer-coarse:h-10"
                      variant={pkg.installed && !pkg.upgradeAvailable ? 'outline' : 'default'}
                      disabled={!isCompat}
                      onClick={() => setInstallTarget({ pkg, version: recVer })}
                    >
                      <Download />
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
  const isPhone = useIsPhoneViewport()

  const versionsList = pkg ? (
    <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-4 pb-6">
      {pkg.versions.map((ver) => (
        <div key={ver.version} className="space-y-2 rounded-xl border border-border bg-card p-3.5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-mono text-sm font-semibold text-foreground">v{ver.version}</span>
              {ver.yanked ? <Badge variant="destructive">{tm('yankedBadge')}</Badge> : null}
              {ver.compatible ? (
                <Badge variant="outline" className="border-success/40 bg-success/10 text-success">
                  {tm('compatOk', { version: ver.version })}
                </Badge>
              ) : (
                <Badge variant="outline" className="border-destructive/40 bg-destructive/10 text-destructive">
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

          <p className="whitespace-pre-wrap text-xs text-muted-foreground">
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
  const isPhone = useIsPhoneViewport()

  const [inspecting, setInspecting] = useState(false)
  const [installing, setInstalling] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [inspect, setInspect] = useState<MarketInspectResult | null>(null)
  const [granted, setGranted] = useState<string[]>([])
  const [allowUnsigned, setAllowUnsigned] = useState(false)
  const [confirmId, setConfirmId] = useState('')
  const seqRef = useRef(0)

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
    setInspecting(true)
    setError(null)
    setInspect(null)
    setAllowUnsigned(false)
    setConfirmId('')

    void locale
    inspectMarketPackage({
      registryId: target.pkg.registryId,
      id: target.pkg.id,
      version: target.version,
    })
      .then((res) => {
        if (seq !== seqRef.current) return
        const data = res.data as MarketInspectResult
        setInspect(data)
        const initGranted = collectInitialGrantedPermissions(data.permissions)
        if (data.runtime === 'native' && !initGranted.includes('native.exec')) {
          initGranted.push('native.exec')
        }
        setGranted(initGranted)
      })
      .catch((err) => {
        if (seq !== seqRef.current) return
        setError(apiProblemMessage(problemOf(err).detail, err, inspectErrRef.current))
      })
      .finally(() => {
        if (seq === seqRef.current) setInspecting(false)
      })
  }, [target, locale])

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
      setError(apiProblemMessage(problemOf(err).detail, err, te('errors.install')))
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
  const totalPermissions =
    (inspect?.permissions.normal.length ?? 0) +
    (inspect?.permissions.sensitive.length ?? 0) +
    (inspect?.permissions.high_risk.length ?? 0)
  const warnings = inspect?.warnings ? Array.from(new Set(inspect.warnings)) : []
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
      <FormError message={error} />

      {inspecting ? (
        <div className="flex items-center justify-center gap-2 py-8 text-sm text-muted-foreground">
          <Spinner />
          <span>{t('verifying')}</span>
        </div>
      ) : inspect ? (
        <div className="space-y-4">
          <div className="flex flex-wrap items-start justify-between gap-3 rounded-lg border border-border bg-card p-3">
            <div className="min-w-0 space-y-1.5">
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-semibold text-foreground">{inspect.name}</span>
                <Badge variant="outline" className="h-auto min-w-0 max-w-full whitespace-normal break-all font-mono">
                  {inspect.id} v{inspect.version}
                </Badge>
                <Badge variant="secondary">
                  {te.has(`kinds.${inspect.kind}`) ? te(`kinds.${inspect.kind}`) : inspect.kind}
                </Badge>
                {inspect.kind === 'plugin' ? (
                  <Badge
                    variant="outline"
                    className={
                      isNative
                        ? 'gap-1 border-primary/40 bg-primary/10 text-primary'
                        : 'gap-1 text-muted-foreground'
                    }
                  >
                    {isNative ? <Cpu className="size-3" /> : <Box className="size-3" />}
                    {te(`runtimes.${isNative ? 'native' : 'js'}`)}
                  </Badge>
                ) : null}
                {inspect.verification.official && inspect.verification.trusted ? (
                  <Badge variant="outline" className="gap-1 border-success/40 bg-success/10 text-success">
                    <BadgeCheck className="size-3" />
                    {t('officialVerified')}
                  </Badge>
                ) : null}
                {inspect.isUpgrade && inspect.currentVersion ? (
                  <Badge variant="outline">{t('upgradeBadge', { version: inspect.currentVersion })}</Badge>
                ) : null}
              </div>
              {inspect.description ? <p className="text-xs text-muted-foreground">{inspect.description}</p> : null}
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground tabular-nums">
                <span>
                  {t('packageSize')}: {formatBytes(inspect.archiveBytes)}
                </span>
                {isNative && inspect.hostPlatform ? (
                  <span className="font-mono">{t('hostPlatformLabel', { platform: inspect.hostPlatform })}</span>
                ) : null}
              </div>
            </div>
          </div>

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
                    {dep.runtime === 'native' ? <Cpu className="size-3 shrink-0 text-primary" /> : <Box className="size-3 shrink-0" />}
                    <span>
                      {dep.name} ({dep.id} v{dep.version})
                    </span>
                  </Badge>
                ))}
              </div>
            </div>
          ) : null}

          {inspect.verification.trusted ? (
            <Alert className="border-success/30 bg-success/10 text-success">
              <ShieldCheck />
              <AlertTitle>
                {t('sigTrusted', {
                  signer:
                    inspect.verification.official &&
                    (!inspect.verification.signer || inspect.verification.signer === 'SkinLoom Official')
                      ? t('officialSigner')
                      : inspect.verification.signer || inspect.verification.keyId || 'Ed25519',
                })}
              </AlertTitle>
            </Alert>
          ) : nativeBlocked ? (
            <Alert variant="destructive">
              <ShieldAlert />
              <AlertTitle>{t('nativeBlockedTitle')}</AlertTitle>
              <AlertDescription className="pt-1">
                {inspect.nativeBlockReason || t('nativeBlockedDefault')}
              </AlertDescription>
            </Alert>
          ) : (
            <Alert variant="destructive">
              <ShieldQuestion />
              <AlertTitle>{inspect.verification.signed ? t('sigUntrusted') : t('sigUnsigned')}</AlertTitle>
              <AlertDescription className="space-y-3 pt-1">
                <p>{t('sigUntrustedWarning')}</p>
                <label className="flex min-h-8 cursor-pointer items-start gap-2 text-xs text-foreground pointer-coarse:min-h-10">
                  <Checkbox checked={allowUnsigned} onCheckedChange={(c) => setAllowUnsigned(Boolean(c))} />
                  <span>{t('allowUnsignedToggle')}</span>
                </label>
              </AlertDescription>
            </Alert>
          )}

          {isNative && !nativeBlocked ? (
            <Alert variant="destructive">
              <Cpu />
              <AlertTitle>{t('nativeRiskTitle')}</AlertTitle>
              <AlertDescription className="pt-1">{t('nativeRiskDescription')}</AlertDescription>
            </Alert>
          ) : null}

          {warnings.length > 0 ? (
            <div className="space-y-1 rounded-lg border border-warning/40 bg-warning/10 p-3 text-xs text-foreground">
              {warnings.map((w, i) => (
                <p key={i}>{w}</p>
              ))}
            </div>
          ) : null}

          {totalPermissions > 0 ? (
            <div className="space-y-3">
              <div>
                <h4 className="text-sm font-medium text-foreground">{t('permissionsTitle')}</h4>
                <p className="text-xs text-muted-foreground">{t('permissionsHint')}</p>
              </div>
              <MarketPermissionTierSection
                label={t('tierNormal')}
                tone="normal"
                items={inspect.permissions.normal}
                granted={granted}
                lockedNames={[]}
                newBadge={t('newPermission')}
                requiredBadge={t('requiredPermission')}
                onToggle={togglePermission}
              />
              <MarketPermissionTierSection
                label={t('tierSensitive')}
                tone="sensitive"
                items={inspect.permissions.sensitive}
                granted={granted}
                lockedNames={[]}
                newBadge={t('newPermission')}
                requiredBadge={t('requiredPermission')}
                onToggle={togglePermission}
              />
              <MarketPermissionTierSection
                label={t('tierHighRisk')}
                tone="high_risk"
                items={inspect.permissions.high_risk}
                granted={granted}
                lockedNames={isNative ? ['native.exec'] : []}
                newBadge={t('newPermission')}
                requiredBadge={t('requiredPermission')}
                onToggle={togglePermission}
              />
            </div>
          ) : null}

          {needsTypedId ? (
            <div className="space-y-1.5 rounded-lg border border-destructive/40 bg-destructive/10 p-3">
              <Label htmlFor="market-confirm-plugin-id" className="text-xs font-medium text-destructive">
                {t('confirmIdLabel', { id: inspect.id })}
              </Label>
              <Input
                id="market-confirm-plugin-id"
                className="font-mono"
                autoComplete="off"
                value={confirmId}
                placeholder={inspect.id}
                onChange={(e) => setConfirmId(e.target.value)}
              />
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  )

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
      <Button
        type="button"
        className="pointer-coarse:h-10"
        onClick={() => void handleConfirmInstall()}
        disabled={!canInstall || installing || inspecting}
      >
        {installing ? <Spinner /> : null}
        {t('confirmInstall')}
      </Button>
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
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t('title')}</DialogTitle>
          <DialogDescription>{target?.pkg.name}</DialogDescription>
        </DialogHeader>
        {body}
        <DialogFooter>{footer}</DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function MarketPermissionTierSection({
  label,
  tone,
  items,
  granted,
  lockedNames,
  newBadge,
  requiredBadge,
  onToggle,
}: {
  label: string
  tone: 'normal' | 'sensitive' | 'high_risk'
  items: PermissionItem[]
  granted: string[]
  lockedNames: string[]
  newBadge: string
  requiredBadge: string
  onToggle: (name: string, enabled: boolean) => void
}) {
  const te = useTranslations('adminPlugins')
  if (items.length === 0) return null
  const borderTone =
    tone === 'high_risk'
      ? 'border-destructive/40 bg-destructive/5'
      : tone === 'sensitive'
        ? 'border-warning/40 bg-warning/5'
        : 'border-border'

  return (
    <div className={`space-y-2 rounded-lg border p-3 ${borderTone}`}>
      <p className="text-xs font-medium text-foreground">{label}</p>
      <div className="space-y-2">
        {items.map((item) => {
          const locked = lockedNames.includes(item.name)
          const permKey = `permissionDescriptions.${permissionMessageKey(item.name)}`
          const desc = te.has(permKey) ? te(permKey) : item.description
          return (
            <label
              key={item.name}
              className={`flex items-start gap-2.5 text-sm pointer-coarse:min-h-10 ${locked ? 'cursor-default' : 'cursor-pointer'}`}
            >
              <Checkbox
                checked={locked || granted.includes(item.name)}
                disabled={locked}
                onCheckedChange={(c) => onToggle(item.name, Boolean(c))}
              />
              <div className="min-w-0 flex-1 space-y-0.5">
                <div className="flex flex-wrap items-center gap-1.5">
                  <code className="min-w-0 break-all text-xs font-medium text-foreground">{item.name}</code>
                  {locked ? <Badge variant="destructive">{requiredBadge}</Badge> : null}
                  {item.added ? <Badge variant="secondary">{newBadge}</Badge> : null}
                </div>
                <p className="text-xs text-muted-foreground">{desc}</p>
              </div>
            </label>
          )
        })}
      </div>
    </div>
  )
}
