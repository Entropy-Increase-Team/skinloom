'use client'

import type { PackageInspectResult, PermissionItem } from '@skinloom/api-client'
import { Alert, AlertDescription, AlertTitle, Badge, Checkbox, Input, Label } from '@skinloom/ui'
import { BadgeCheck, Box, Cpu, ShieldAlert, ShieldCheck, ShieldQuestion } from 'lucide-react'
import { useLocale, useTranslations } from 'next-intl'
import type { ReactNode } from 'react'

import { formatBytes } from '@/lib/bytes'

import { permissionMessageKey } from './plugins-settings-form'

export function PackageInspectHeader({
  inspect,
  action,
  showPlatforms = false,
}: {
  inspect: PackageInspectResult
  action?: ReactNode
  showPlatforms?: boolean
}) {
  const t = useTranslations('adminPlugins.inspect')
  const te = useTranslations('adminPlugins')
  const locale = useLocale()
  const isNative = inspect.runtime === 'native'

  return (
    <div className="flex flex-wrap items-start justify-between gap-3 rounded-lg border border-border bg-card p-3">
      <div className="min-w-0 space-y-1.5">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-medium text-foreground">{inspect.name}</span>
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
              {isNative ? <Cpu className="size-3" aria-hidden /> : <Box className="size-3" aria-hidden />}
              {te(`runtimes.${isNative ? 'native' : 'js'}`)}
            </Badge>
          ) : null}
          {inspect.verification.official && inspect.verification.trusted ? (
            <Badge variant="outline" className="gap-1 border-success/40 bg-success/10 text-success">
              <BadgeCheck className="size-3" aria-hidden />
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
            {t('packageSize')}: {formatBytes(inspect.archiveBytes, locale)}
          </span>
          {isNative && inspect.hostPlatform ? (
            <span className="font-mono">{t('hostPlatformLabel', { platform: inspect.hostPlatform })}</span>
          ) : null}
        </div>
        {showPlatforms && isNative && inspect.supportedPlatforms && inspect.supportedPlatforms.length > 0 ? (
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
      {action}
    </div>
  )
}

export function PackageTrustAndRiskAlerts({
  inspect,
  allowUnsigned,
  disabled,
  onAllowUnsignedChange,
}: {
  inspect: PackageInspectResult
  allowUnsigned: boolean
  disabled?: boolean
  onAllowUnsignedChange: (checked: boolean) => void
}) {
  const t = useTranslations('adminPlugins.inspect')
  const isNative = inspect.runtime === 'native'
  const nativeBlocked = Boolean(inspect.nativeInstallBlocked)
  const platformIncompatible = Boolean(isNative && !inspect.platformCompatible)
  const warnings = inspect.warnings ? Array.from(new Set(inspect.warnings)) : []

  return (
    <>
      {inspect.verification.trusted ? (
        <Alert variant="success">
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
              <Checkbox checked={allowUnsigned} disabled={disabled} onCheckedChange={(c) => onAllowUnsignedChange(Boolean(c))} />
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
            <p key={i} className="break-words">
              {w}
            </p>
          ))}
        </div>
      ) : null}
    </>
  )
}

export function PackagePermissionsPanel({
  inspect,
  granted,
  disabled,
  onToggle,
}: {
  inspect: PackageInspectResult
  granted: string[]
  disabled?: boolean
  onToggle: (name: string, enabled: boolean) => void
}) {
  const t = useTranslations('adminPlugins.inspect')
  const isNative = inspect.runtime === 'native'
  const totalPermissions =
    inspect.permissions.normal.length + inspect.permissions.sensitive.length + inspect.permissions.high_risk.length
  if (totalPermissions === 0) return null

  return (
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
        disabled={disabled}
        newBadge={t('newPermission')}
        requiredBadge={t('requiredPermission')}
        onToggle={onToggle}
      />
      <PermissionTierSection
        label={t('tierSensitive')}
        tone="sensitive"
        items={inspect.permissions.sensitive}
        granted={granted}
        lockedNames={[]}
        disabled={disabled}
        newBadge={t('newPermission')}
        requiredBadge={t('requiredPermission')}
        onToggle={onToggle}
      />
      <PermissionTierSection
        label={t('tierHighRisk')}
        tone="high_risk"
        items={inspect.permissions.high_risk}
        granted={granted}
        lockedNames={isNative ? ['native.exec'] : []}
        disabled={disabled}
        newBadge={t('newPermission')}
        requiredBadge={t('requiredPermission')}
        onToggle={onToggle}
      />
    </div>
  )
}

export function TypedConfirmPluginId({
  id,
  inputId,
  value,
  disabled,
  onChange,
}: {
  id: string
  inputId: string
  value: string
  disabled?: boolean
  onChange: (next: string) => void
}) {
  const t = useTranslations('adminPlugins.inspect')
  return (
    <div className="space-y-1.5 rounded-lg border border-destructive/40 bg-destructive/10 p-3">
      <Label htmlFor={inputId} className="text-xs leading-snug font-medium break-words text-destructive">
        {t('confirmIdLabel', { id })}
      </Label>
      <Input
        id={inputId}
        className="font-mono"
        autoComplete="off"
        disabled={disabled}
        value={value}
        placeholder={id}
        onChange={(e) => onChange(e.target.value)}
      />
    </div>
  )
}

function PermissionTierSection({
  label,
  tone,
  items,
  granted,
  lockedNames,
  disabled,
  newBadge,
  requiredBadge,
  onToggle,
}: {
  label: string
  tone: 'normal' | 'sensitive' | 'high_risk'
  items: PermissionItem[]
  granted: string[]
  lockedNames: string[]
  disabled?: boolean
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
          const itemDisabled = locked || Boolean(disabled)
          const permKey = `permissionDescriptions.${permissionMessageKey(item.name)}`
          const desc = te.has(permKey) ? te(permKey) : item.description
          return (
            <label
              key={item.name}
              className={`flex items-start gap-2.5 text-sm pointer-coarse:min-h-10 ${itemDisabled ? 'cursor-default' : 'cursor-pointer'}`}
            >
              <Checkbox
                checked={locked || granted.includes(item.name)}
                disabled={itemDisabled}
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
