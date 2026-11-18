'use client'

import { Button, cn, RadioGroup, RadioGroupItem } from '@skinloom/ui'
import { ArchiveRestore, ArrowRightLeft, Sparkles } from 'lucide-react'
import { useTranslations } from 'next-intl'

import type { SetupMode } from './types'

const MODES: { id: SetupMode; icon: typeof Sparkles }[] = [
  { id: 'fresh', icon: Sparkles },
  { id: 'bs', icon: ArrowRightLeft },
  { id: 'restore', icon: ArchiveRestore },
]

export function ModeStep({
  mode,
  onChange,
  onNext,
}: {
  mode: SetupMode
  onChange: (mode: SetupMode) => void
  onNext: () => void
}) {
  const t = useTranslations('setup.mode')

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h2 className="text-lg font-medium">{t('title')}</h2>
        <p className="text-sm text-muted-foreground">{t('intro')}</p>
      </div>

      <RadioGroup
        value={mode}
        onValueChange={(v) => onChange(v as SetupMode)}
        className="grid gap-3"
      >
        {MODES.map(({ id, icon: Icon }) => {
          const selected = mode === id
          return (
            <label
              key={id}
              htmlFor={`setup-mode-${id}`}
              className={cn(
                'flex cursor-pointer items-start gap-3.5 rounded-xl border p-4 transition-colors',
                selected ? 'border-primary bg-primary/5' : 'bg-card hover:bg-muted/40',
              )}
            >
              <RadioGroupItem id={`setup-mode-${id}`} value={id} className="mt-1" />
              <span
                className={cn(
                  'flex size-9 shrink-0 items-center justify-center rounded-lg',
                  selected ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground',
                )}
              >
                <Icon className="size-4" aria-hidden />
              </span>
              <div className="flex min-w-0 flex-1 flex-col gap-1">
                <span className="text-sm font-medium">{t(`${id}.title`)}</span>
                <span className="text-xs leading-relaxed text-muted-foreground">{t(`${id}.desc`)}</span>
              </div>
            </label>
          )
        })}
      </RadioGroup>

      <div className="flex justify-end">
        <Button onClick={onNext}>{t('next')}</Button>
      </div>
    </div>
  )
}
