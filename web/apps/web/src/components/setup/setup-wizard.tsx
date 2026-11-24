'use client'

import {
  type DBConfigInput,
  getSetupJob,
  getSetupState,
  type SetupJobView,
  type SetupState,
  type StorageConfigInput,
} from '@skinloom/api-client'
import { Card, CardContent, CardDescription, CardHeader, CardTitle, Skeleton } from '@skinloom/ui'
import { useRouter } from 'next/navigation'
import { useTranslations } from 'next-intl'
import { useCallback, useEffect, useMemo, useState } from 'react'

import { BSMigrateStep } from './bs-migrate-step'
import { InfraStep } from './infra-step'
import { ModeStep } from './mode-step'
import { NewSiteStep } from './new-site-step'
import { RestoreStep } from './restore-step'
import { TokenGate } from './token-gate'
import { initialDBConfig, initialStorageConfig, type SetupMode, type WizardStep } from './types'

export function SetupWizard({ initialToken }: { initialToken: string }) {
  const t = useTranslations('setup.wizard')
  const router = useRouter()

  const [checking, setChecking] = useState(true)
  const [step, setStep] = useState<WizardStep>('gate')
  const [state, setState] = useState<SetupState | null>(null)
  const [mode, setMode] = useState<SetupMode>('fresh')
  const [db, setDb] = useState<DBConfigInput>({ driver: 'sqlite', sqlitePath: 'skinloom.db' })
  const [storage, setStorage] = useState<StorageConfigInput>({ driver: 'local', path: 'textures' })
  const [trustProxy, setTrustProxy] = useState(false)

  const applyState = useCallback(async (next: SetupState) => {
    setState(next)
    setDb(initialDBConfig(next))
    setStorage(initialStorageConfig(next))
    setTrustProxy(Boolean(next.detectedProxy?.suggestedCidr))
    if (next.activeJobId) {
      try {
        const res = await getSetupJob(next.activeJobId)
        const j = res.data as SetupJobView
        if (j.kind === 'restore') {
          setMode('restore')
          setStep('branch')
          return
        }
        if (j.kind === 'bs_dry_run' || j.kind === 'bs_import') {
          setMode('bs')
          setStep('branch')
          return
        }
      } catch {
        // Fall through to mode step
      }
    }
    setStep('mode')
  }, [])

  useEffect(() => {
    let active = true
    void (async () => {
      try {
        const res = await getSetupState()
        if (!active) return
        await applyState(res.data as SetupState)
      } catch {
        if (active) setStep('gate')
      } finally {
        if (active) setChecking(false)
      }
    })()
    return () => {
      active = false
    }
  }, [applyState])

  const trustedProxies = useMemo(() => {
    if (!state) return []
    if (state.envLocked.trustedProxies) return state.trustedProxies
    const cidr = state.detectedProxy?.suggestedCidr
    if (trustProxy && cidr) {
      return Array.from(new Set([...state.trustedProxies, cidr]))
    }
    return cidr ? state.trustedProxies.filter((p) => p !== cidr) : state.trustedProxies
  }, [state, trustProxy])

  const handleCompleted = useCallback(
    (redirectTo: string) => {
      router.replace(redirectTo || '/')
      router.refresh()
    },
    [router],
  )

  const stepIndex = step === 'gate' ? 0 : step === 'mode' ? 1 : step === 'infra' ? 2 : 3
  const stepLabels = [t('steps.verify'), t('steps.mode'), t('steps.infra'), t('steps.configure')]

  return (
    <Card>
      <CardHeader className="gap-4">
        <div className="flex flex-col gap-1">
          <CardTitle as="h1">{t('title')}</CardTitle>
          <CardDescription>{t('subtitle')}</CardDescription>
        </div>
        <ol className="flex flex-wrap items-center gap-2 text-xs">
          {stepLabels.map((label, idx) => (
            <li
              key={label}
              className={`rounded-md px-2.5 py-1 tabular-nums ${
                idx === stepIndex
                  ? 'bg-primary text-primary-foreground font-medium'
                  : idx < stepIndex
                    ? 'bg-muted text-foreground'
                    : 'bg-muted/40 text-muted-foreground'
              }`}
            >
              {idx + 1}. {label}
            </li>
          ))}
        </ol>
      </CardHeader>
      <CardContent>
        {checking ? (
          <Skeleton className="h-64 rounded-xl" />
        ) : step === 'gate' || !state ? (
          <TokenGate initialToken={initialToken} onVerified={applyState} />
        ) : step === 'mode' ? (
          <ModeStep mode={mode} onChange={setMode} onNext={() => setStep('infra')} />
        ) : step === 'infra' ? (
          <InfraStep
            state={state}
            db={db}
            storage={storage}
            trustProxy={trustProxy}
            onChangeDB={setDb}
            onChangeStorage={setStorage}
            onChangeTrustProxy={setTrustProxy}
            onSwitchToBS={() => setMode('bs')}
            onBack={() => setStep('mode')}
            onNext={() => setStep('branch')}
            onConnectedExisting={handleCompleted}
          />
        ) : mode === 'fresh' ? (
          <NewSiteStep
            state={state}
            db={db}
            storage={storage}
            trustedProxies={trustedProxies}
            onBack={() => setStep('infra')}
            onCompleted={handleCompleted}
          />
        ) : mode === 'restore' ? (
          <RestoreStep
            state={state}
            db={db}
            storage={storage}
            trustedProxies={trustedProxies}
            initialJobId={state.activeJobId}
            onBack={() => setStep('infra')}
            onCompleted={handleCompleted}
          />
        ) : (
          <BSMigrateStep
            state={state}
            db={db}
            storage={storage}
            trustedProxies={trustedProxies}
            initialJobId={state.activeJobId}
            onBack={() => setStep('infra')}
            onCompleted={handleCompleted}
          />
        )}
      </CardContent>
    </Card>
  )
}
