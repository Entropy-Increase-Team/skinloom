'use client'

import { type Block, type BlockType, type Prop, type SettingsList, type ThemeView, updateSettings } from '@skinloom/api-client'
import { Button, Card, CardContent, CardDescription, CardHeader, CardTitle, Input } from '@skinloom/ui'
import { ArrowDown, ArrowUp, Plus, Trash2 } from 'lucide-react'
import { useLocale, useTranslations } from 'next-intl'
import { useState } from 'react'

import { FormError } from '@/components/auth/field'
import { problemOf } from '@/lib/problem'

import { type EditBlock, editBlocks, localizedOf, localizedValue, moved, savedBlocks } from './appearance-form'
import { equal, LOCALES } from './settings-form'

const selectClass = 'h-9 rounded-md border border-input bg-transparent px-2 text-sm'
const areaClass = 'rounded-md border border-input bg-transparent px-3 py-2 text-sm'
const MAX_BUTTONS = 6

/**
 * The home page's blocks: home.blocks when set, otherwise the active theme's own list,
 * which the editor starts from. Saving stores the list; "use the theme's" sets it back to
 * null, so a later theme switch brings that theme's home page.
 */
export function HomeEditor({
  theme,
  types,
  stored,
  onSaved,
}: {
  theme: ThemeView
  types: BlockType[]
  stored: Block[] | null
  onSaved: (l: SettingsList) => void
}) {
  const t = useTranslations('appearance.home')
  const tb = useTranslations('appearance.blockTypes')
  const locale = useLocale()
  const initial = editBlocks(stored ?? theme.home)
  const [blocks, setBlocks] = useState(initial)
  const [adding, setAdding] = useState(types[0]?.type ?? '')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const dirty = !equal(savedBlocks(blocks, types), savedBlocks(initial, types))

  async function save(value: Block[] | null) {
    setPending(true)
    setError(null)
    try {
      onSaved((await updateSettings({ 'home.blocks': value })).data as SettingsList)
    } catch (err) {
      // The server checks the blocks against the catalog and says which one is wrong.
      const detail = problemOf(err).fields['home.blocks']
      setError(detail ? t('invalid', { detail }) : t('failed'))
      setPending(false)
    }
  }

  const name = (type: string) => (tb.has(`${type}.name`) ? tb(`${type}.name`) : type)
  const update = (i: number, b: EditBlock) => setBlocks((list) => list.map((x, j) => (j === i ? b : x)))
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('title')}</CardTitle>
        <CardDescription>
          {t('intro')} {stored ? t('custom') : t('usingTheme', { theme: theme.name[locale] ?? theme.name.en ?? theme.id })}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {blocks.length === 0 && <p className="text-sm text-muted-foreground">{t('empty')}</p>}
        {blocks.map((b, i) => (
          <BlockCard
            key={i}
            block={b}
            type={types.find((x) => x.type === b.type)}
            title={name(b.type)}
            first={i === 0}
            last={i === blocks.length - 1}
            onChange={(next) => update(i, next)}
            onMove={(delta) => setBlocks((list) => moved(list, i, delta))}
            onRemove={() => setBlocks((list) => list.filter((_, j) => j !== i))}
          />
        ))}
        <div className="flex flex-wrap items-center gap-2">
          <select value={adding} onChange={(e) => setAdding(e.target.value)} aria-label={t('addType')} className={selectClass}>
            {types.map((x) => (
              <option key={x.type} value={x.type}>
                {name(x.type)}
              </option>
            ))}
          </select>
          <Button variant="outline" size="sm" disabled={!adding} onClick={() => setBlocks((list) => [...list, { type: adding, props: {} }])}>
            <Plus />
            {t('add')}
          </Button>
        </div>
        <FormError message={error} />
        <div className="flex flex-wrap items-center gap-3">
          <Button disabled={pending || !dirty} onClick={() => void save(savedBlocks(blocks, types))}>
            {t('save')}
          </Button>
          {stored && (
            <Button variant="outline" disabled={pending} onClick={() => void save(null)}>
              {t('useTheme')}
            </Button>
          )}
          {dirty && <span className="text-xs text-muted-foreground">{t('unsaved')}</span>}
        </div>
      </CardContent>
    </Card>
  )
}

function BlockCard({
  block,
  type,
  title,
  first,
  last,
  onChange,
  onMove,
  onRemove,
}: {
  block: EditBlock
  type: BlockType | undefined
  title: string
  first: boolean
  last: boolean
  onChange: (b: EditBlock) => void
  onMove: (delta: -1 | 1) => void
  onRemove: () => void
}) {
  const t = useTranslations('appearance.home')
  const tb = useTranslations('appearance.blockTypes')
  const set = (name: string, v: unknown) => onChange({ ...block, props: { ...block.props, [name]: v } })
  return (
    <div className="flex flex-col gap-3 rounded-lg border p-4">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{title}</span>
        <code className="font-mono text-xs text-muted-foreground">{block.type}</code>
        <div className="ml-auto flex gap-1">
          <Button variant="ghost" size="icon" aria-label={t('moveUp')} disabled={first} onClick={() => onMove(-1)}>
            <ArrowUp />
          </Button>
          <Button variant="ghost" size="icon" aria-label={t('moveDown')} disabled={last} onClick={() => onMove(1)}>
            <ArrowDown />
          </Button>
          <Button variant="ghost" size="icon" aria-label={t('remove')} onClick={onRemove}>
            <Trash2 />
          </Button>
        </div>
      </div>
      {tb.has(`${block.type}.help`) && <p className="text-xs text-muted-foreground">{tb(`${block.type}.help`)}</p>}
      {!type && <p className="text-xs text-destructive">{t('unknownType')}</p>}
      {type?.props.map((p) => (
        <PropField key={p.name} prop={p} value={block.props[p.name]} onChange={(v) => set(p.name, v)} />
      ))}
    </div>
  )
}

/** One property, edited by its kind (internal/theme PropType). */
function PropField({ prop: p, value, onChange }: { prop: Prop; value: unknown; onChange: (v: unknown) => void }) {
  const t = useTranslations('appearance')
  const label = t.has(`props.${p.name}`) ? t(`props.${p.name}`) : p.name
  const option = (v: string) => (t.has(`options.${v}`) ? t(`options.${v}`) : v)

  let control: React.ReactNode
  switch (p.type) {
    case 'text':
    case 'markdown':
      control = <LocalizedInput multiline={p.type === 'markdown'} value={value} onChange={onChange} />
      break
    case 'link':
    case 'image':
      control = (
        <Input value={typeof value === 'string' ? value : ''} placeholder={t('home.linkHint')} onChange={(e) => onChange(e.target.value)} />
      )
      break
    case 'number':
      control = (
        <Input
          type="number"
          min={p.min}
          max={p.max}
          value={typeof value === 'number' ? String(value) : ''}
          placeholder={t('home.default')}
          onChange={(e) => onChange(e.target.value === '' ? undefined : Number(e.target.value))}
          className="w-32"
        />
      )
      break
    case 'choice':
      control = (
        <select value={typeof value === 'string' ? value : ''} onChange={(e) => onChange(e.target.value || undefined)} className={`${selectClass} w-fit`}>
          <option value="">{t('home.default')}</option>
          {p.options?.map((o) => (
            <option key={o} value={o}>
              {option(o)}
            </option>
          ))}
        </select>
      )
      break
    case 'buttons':
      control = <ButtonsInput value={value} onChange={onChange} />
      break
  }
  return (
    <div className="flex flex-col gap-1">
      <span className="text-xs font-medium text-muted-foreground">
        {label}
        {p.type === 'number' && ` (${p.min}–${p.max})`}
      </span>
      {control}
    </div>
  )
}

/** Text in each editing language; empty languages are not stored. */
function LocalizedInput({ value, multiline, onChange }: { value: unknown; multiline: boolean; onChange: (v: unknown) => void }) {
  const t = useTranslations('adminSettings.locales')
  const texts = localizedOf(value)
  const set = (l: string, s: string) => onChange(localizedValue({ ...texts, [l]: s }))
  return (
    <div className="grid gap-2 sm:grid-cols-2">
      {LOCALES.map((l) =>
        multiline ? (
          <textarea key={l} rows={4} aria-label={t(l)} placeholder={t(l)} value={texts[l]} onChange={(e) => set(l, e.target.value)} className={areaClass} />
        ) : (
          <Input key={l} aria-label={t(l)} placeholder={t(l)} value={texts[l]} onChange={(e) => set(l, e.target.value)} />
        ),
      )}
    </div>
  )
}

type ButtonItem = { label?: unknown; href?: string }

function ButtonsInput({ value, onChange }: { value: unknown; onChange: (v: unknown) => void }) {
  const t = useTranslations('appearance.home')
  const items = (Array.isArray(value) ? value : []) as ButtonItem[]
  const set = (i: number, item: ButtonItem) => onChange(items.map((x, j) => (j === i ? item : x)))
  return (
    <div className="flex flex-col gap-3">
      {items.map((item, i) => (
        <div key={i} className="flex flex-col gap-2 rounded-md bg-muted/40 p-3">
          <LocalizedInput multiline={false} value={item.label} onChange={(label) => set(i, { ...item, label })} />
          <div className="flex gap-2">
            <Input value={item.href ?? ''} placeholder={t('linkHint')} onChange={(e) => set(i, { ...item, href: e.target.value })} />
            <Button variant="ghost" size="icon" aria-label={t('remove')} onClick={() => onChange(items.filter((_, j) => j !== i))}>
              <Trash2 />
            </Button>
          </div>
        </div>
      ))}
      {items.length < MAX_BUTTONS && (
        <Button variant="outline" size="sm" className="self-start" onClick={() => onChange([...items, { label: undefined, href: '' }])}>
          <Plus />
          {t('addButton')}
        </Button>
      )}
    </div>
  )
}
