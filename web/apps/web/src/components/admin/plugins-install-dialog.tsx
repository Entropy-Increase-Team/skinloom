'use client'

import {
  type AdminPluginView,
  inspectPluginFile,
  installPluginFile,
  type PackageInspectResult,
} from '@skinloom/api-client'
import {
  Alert,
  AlertTitle,
  Badge,
  Button,
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
  Spinner,
  toast,
  useIsMobile,
} from '@skinloom/ui'
import { ShieldAlert } from 'lucide-react'
import { useTranslations } from 'next-intl'
import { useEffect, useRef, useState } from 'react'

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
} from './plugins-settings-form'

const MAX_PACKAGE_BYTES = 150 * 1024 * 1024

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
  const isPhone = useIsMobile()

  const [file, setFile] = useState<File | null>(null)
  const [inspecting, setInspecting] = useState(false)
  const [installing, setInstalling] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [inspect, setInspect] = useState<PackageInspectResult | null>(null)
  const [granted, setGranted] = useState<string[]>([])
  const [allowUnsigned, setAllowUnsigned] = useState(false)
  const [confirmId, setConfirmId] = useState('')
  const inspectSeqRef = useRef(0)
  const inspectAbortRef = useRef<AbortController | null>(null)
  const errorBoxRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    return () => {
      inspectAbortRef.current?.abort()
    }
  }, [])

  function resetState() {
    inspectSeqRef.current += 1
    inspectAbortRef.current?.abort()
    inspectAbortRef.current = null
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
    inspectAbortRef.current?.abort()
    const ctl = new AbortController()
    inspectAbortRef.current = ctl
    setFile(nextFile)
    setInspecting(true)
    try {
      const res = await inspectPluginFile(nextFile, ctl.signal)
      if (seq !== inspectSeqRef.current || ctl.signal.aborted) return
      setInspect(res)
      const initGranted = collectInitialGrantedPermissions(res.permissions)
      if (res.runtime === 'native' && !initGranted.includes('native.exec')) {
        initGranted.push('native.exec')
      }
      setGranted(initGranted)
    } catch (err) {
      if (seq !== inspectSeqRef.current || ctl.signal.aborted || (err instanceof DOMException && err.name === 'AbortError')) {
        return
      }
      setError(apiProblemMessage(problemOf(err).detail, err, te('errors.inspect')))
    } finally {
      if (seq === inspectSeqRef.current && !ctl.signal.aborted) setInspecting(false)
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

      <div ref={errorBoxRef}>
        <FormError message={error} />
      </div>

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
          <PackageInspectHeader
            inspect={inspect}
            showPlatforms
            action={
              <Button type="button" variant="ghost" size="sm" className="pointer-coarse:h-10" onClick={resetState} disabled={installing}>
                {t('startOver')}
              </Button>
            }
          />

          <PackageTrustAndRiskAlerts
            inspect={inspect}
            allowUnsigned={allowUnsigned}
            disabled={installing}
            onAllowUnsignedChange={setAllowUnsigned}
          />

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

          <PackagePermissionsPanel
            inspect={inspect}
            granted={granted}
            disabled={installing}
            onToggle={togglePermission}
          />

          {needsTypedId ? (
            <TypedConfirmPluginId
              id={inspect.id}
              inputId="confirm-plugin-id"
              value={confirmId}
              disabled={installing}
              onChange={setConfirmId}
            />
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
      <Button type="button" className="pointer-coarse:h-10" loading={installing} onClick={handleInstall} disabled={!canInstall}>
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
      <DialogContent showCloseButton={!installing} className="flex max-h-[90dvh] flex-col overflow-hidden sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t('title')}</DialogTitle>
          <DialogDescription>{t('dropHint')}</DialogDescription>
        </DialogHeader>
        <div className="min-h-0 flex-1 overflow-y-auto pr-1">{body}</div>
        <DialogFooter>{footer}</DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
