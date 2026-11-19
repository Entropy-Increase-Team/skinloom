'use client'

import {
  type AdminPluginView,
  inspectPluginFile,
  installPluginFile,
  type PackageInspectResult,
  type PermissionItem,
} from '@skinloom/api-client'
import {
  Alert,
  AlertDescription,
  AlertTitle,
  Badge,
  Button,
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
  Dropzone,
  Input,
  Label,
  Spinner,
  toast,
} from '@skinloom/ui'
import { BadgeCheck, Box, Cpu, ShieldAlert, ShieldCheck, ShieldQuestion } from 'lucide-react'
import { useTranslations } from 'next-intl'
import { useEffect, useRef, useState } from 'react'

import { FormError } from '@/components/auth/field'
import { problemOf } from '@/lib/problem'

import {
  apiProblemMessage,
  canSubmitPackageInstall,
  collectInitialGrantedPermissions,
  formatBytes,
} from './plugins-settings-form'

const MAX_PACKAGE_BYTES = 150 * 1024 * 1024

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

export function PluginInstallDialog({
  open,
  superadmin,
  onOpenChange,
  onInstalled,
}: {
  open: boolean
  superadmin: boolean
  onOpenChange: (open: boolean) => void
  onInstalled: (plugin: AdminPluginView) => void
}) {
  const t = useTranslations('adminPlugins.inspect')
  const te = useTranslations('adminPlugins')
  const isPhone = useIsPhoneViewport()

  const [file, setFile] = useState<File | null>(null)
  const [inspecting, setInspecting] = useState(false)
  const [installing, setInstalling] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [inspect, setInspect] = useState<PackageInspectResult | null>(null)
  const [granted, setGranted] = useState<string[]>([])
  const [allowUnsigned, setAllowUnsigned] = useState(false)
  const [confirmId, setConfirmId] = useState('')
  const inspectSeqRef = useRef(0)

  function resetState() {
    inspectSeqRef.current += 1
    setFile(null)
    setInspect(null)
    setInspecting(false)
    setError(null)
    setGranted([])
    setAllowUnsigned(false)
    setConfirmId('')
  }

  async function handlePickedFile(nextFile: File) {
    setError(null)
    setInspect(null)
    setAllowUnsigned(false)
    setConfirmId('')
    if (!nextFile.name.toLowerCase().endsWith('.slp')) {
      setFile(null)
      setError(t('invalidPackageType'))
      return
    }
    if (nextFile.size <= 0 || nextFile.size > MAX_PACKAGE_BYTES) {
      setFile(null)
      setError(t('packageTooLarge'))
      return
    }
    const seq = ++inspectSeqRef.current
    setFile(nextFile)
    setInspecting(true)
    try {
      const res = await inspectPluginFile(nextFile)
      if (seq !== inspectSeqRef.current) return
      setInspect(res)
      const initGranted = collectInitialGrantedPermissions(res.permissions)
      if (res.runtime === 'native' && !initGranted.includes('native.exec')) {
        initGranted.push('native.exec')
      }
      setGranted(initGranted)
    } catch (err) {
      if (seq !== inspectSeqRef.current) return
      setError(apiProblemMessage(problemOf(err).detail, err, te('errors.inspect')))
    } finally {
      if (seq === inspectSeqRef.current) setInspecting(false)
    }
  }

  async function handleInstall() {
    if (!file || !inspect) return
    setInstalling(true)
    setError(null)
    try {
      const installed = await installPluginFile({
        file,
        grantedPermissions: granted,
        allowUnsigned,
      })
      toast.success(te('toasts.installed', { id: installed.id, version: installed.version }))
      resetState()
      onInstalled(installed)
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
  const hasHighRiskGranted = isNative || (inspect?.permissions.high_risk.some((p) => granted.includes(p.name)) ?? false)
  const needsTypedId = Boolean(
    inspect &&
      !nativeBlocked &&
      !platformIncompatible &&
      (!inspect.verification.trusted || ((inspect.requiresHighRiskConfirm || isNative) && hasHighRiskGranted)),
  )
  const canInstall = superadmin && canSubmitPackageInstall(inspect, granted, allowUnsigned, confirmId)

  function handleOpenChange(next: boolean) {
    if (!next) {
      if (installing) return
      resetState()
    }
    onOpenChange(next)
  }

  const body = (
    <div className="space-y-4">
      {!superadmin ? (
        <Alert variant="destructive">
          <ShieldAlert />
          <AlertTitle>{t('superadminRequired')}</AlertTitle>
        </Alert>
      ) : null}

      <FormError message={error} />

      {!inspect ? (
        <Dropzone
          accept=".slp,application/zip"
          disabled={!superadmin || inspecting}
          hint={t('dropTitle')}
          onFile={handlePickedFile}
        >
          {inspecting ? (
            <div className="flex items-center justify-center gap-2 py-4 text-sm text-muted-foreground">
              <Spinner />
              <span>{t('verifying')}</span>
            </div>
          ) : null}
        </Dropzone>
      ) : (
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
              {isNative && inspect.supportedPlatforms && inspect.supportedPlatforms.length > 0 ? (
                <div className="flex flex-wrap items-center gap-1.5 pt-0.5 text-xs">
                  <span className="text-muted-foreground">{t('platformsLabel')}:</span>
                  {inspect.supportedPlatforms.map((plat) => (
                    <Badge key={plat} variant="secondary" className="font-mono text-xs">
                      {plat}
                    </Badge>
                  ))}
                </div>
              ) : null}
            </div>
            <Button type="button" variant="ghost" size="sm" className="pointer-coarse:h-10" onClick={resetState} disabled={installing}>
              {t('startOver')}
            </Button>
          </div>

          {inspect.verification.trusted ? (
            <Alert className="border-success/30 bg-success/10 text-success">
              <ShieldCheck />
              <AlertTitle>{t('sigTrusted', { signer: inspect.verification.signer || inspect.verification.keyId || 'Ed25519' })}</AlertTitle>
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

          {platformIncompatible ? (
            <Alert variant="destructive">
              <ShieldAlert />
              <AlertTitle>{t('platformIncompatibleTitle', { platform: inspect.hostPlatform })}</AlertTitle>
              <AlertDescription className="pt-1">
                {t('platformIncompatibleDescription', {
                  platform: inspect.hostPlatform,
                  supported: (inspect.supportedPlatforms ?? []).join(', ') || '—',
                })}
              </AlertDescription>
            </Alert>
          ) : null}

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

          <div className="flex flex-wrap gap-2 text-xs">
            {inspect.hasServer ? (
              <Badge variant="outline">{isNative ? t('nativeRuntime') : t('serverRuntime')}</Badge>
            ) : null}
            {inspect.hasWeb ? <Badge variant="outline">{t('webUi')}</Badge> : null}
            {inspect.routesCount > 0 ? <Badge variant="outline" className="tabular-nums">{t('routesCount', { count: inspect.routesCount })}</Badge> : null}
            {inspect.hooksCount > 0 ? <Badge variant="outline" className="tabular-nums">{t('hooksCount', { count: inspect.hooksCount })}</Badge> : null}
            {inspect.slots && inspect.slots.length > 0 ? (
              <Badge variant="outline" className="tabular-nums">{t('slotsCount', { count: inspect.slots.length })}</Badge>
            ) : null}
          </div>

          {totalPermissions > 0 ? (
            <div className="space-y-3">
              <div>
                <h4 className="text-sm font-medium text-foreground">{t('permissionsTitle')}</h4>
                <p className="text-xs text-muted-foreground">{t('permissionsHint')}</p>
              </div>
              <PermissionTierSection
                label={t('tierNormal')}
                tone="normal"
                items={inspect.permissions.normal}
                granted={granted}
                lockedNames={[]}
                newBadge={t('newPermission')}
                requiredBadge={t('requiredPermission')}
                onToggle={togglePermission}
              />
              <PermissionTierSection
                label={t('tierSensitive')}
                tone="sensitive"
                items={inspect.permissions.sensitive}
                granted={granted}
                lockedNames={[]}
                newBadge={t('newPermission')}
                requiredBadge={t('requiredPermission')}
                onToggle={togglePermission}
              />
              <PermissionTierSection
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
              <Label htmlFor="confirm-plugin-id" className="text-xs font-medium text-destructive">
                {t('confirmIdLabel', { id: inspect.id })}
              </Label>
              <Input
                id="confirm-plugin-id"
                className="font-mono"
                autoComplete="off"
                value={confirmId}
                placeholder={inspect.id}
                onChange={(e) => setConfirmId(e.target.value)}
              />
            </div>
          ) : null}
        </div>
      )}
    </div>
  )

  const footer = (
    <>
      <Button type="button" variant="outline" className="pointer-coarse:h-10" onClick={() => handleOpenChange(false)} disabled={installing}>
        {t('cancel')}
      </Button>
      <Button type="button" className="pointer-coarse:h-10" onClick={handleInstall} disabled={!canInstall || installing}>
        {installing ? <Spinner /> : null}
        {t('confirmInstall')}
      </Button>
    </>
  )

  if (isPhone) {
    return (
      <Drawer open={open} onOpenChange={handleOpenChange}>
        <DrawerContent>
          <DrawerHeader>
            <DrawerTitle>{t('title')}</DrawerTitle>
            <DrawerDescription>{t('dropHint')}</DrawerDescription>
          </DrawerHeader>
          <div className="min-h-0 flex-1 overflow-y-auto px-4 pt-3 pb-4">{body}</div>
          <DrawerFooter className="border-t pt-3 pb-[max(1rem,env(safe-area-inset-bottom))]">{footer}</DrawerFooter>
        </DrawerContent>
      </Drawer>
    )
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t('title')}</DialogTitle>
          <DialogDescription>{t('dropHint')}</DialogDescription>
        </DialogHeader>
        {body}
        <DialogFooter>{footer}</DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function PermissionTierSection({
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
  if (items.length === 0) return null
  const borderTone =
    tone === 'high_risk' ? 'border-destructive/40 bg-destructive/5' : tone === 'sensitive' ? 'border-warning/40 bg-warning/5' : 'border-border'

  return (
    <div className={`space-y-2 rounded-lg border p-3 ${borderTone}`}>
      <p className="text-xs font-medium text-foreground">{label}</p>
      <div className="space-y-2">
        {items.map((item) => {
          const locked = lockedNames.includes(item.name)
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
                <p className="text-xs text-muted-foreground">{item.description}</p>
              </div>
            </label>
          )
        })}
      </div>
    </div>
  )
}
