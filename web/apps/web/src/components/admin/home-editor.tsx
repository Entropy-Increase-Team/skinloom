'use client'

import { type Block, type BlockType, type Prop, type SettingsList, type ThemeView, updateSettings } from '@skinloom/api-client'
import {
  Button,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Input,
  SaveBar,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Textarea,
  toast,
} from '@skinloom/ui'
import { ArrowDown, ArrowUp, ChevronRight, Plus, Trash2 } from 'lucide-react'
import { useLocale, useTranslations } from 'next-intl'
import { useState } from 'react'

import { FormError } from '@/components/auth/field'
import { problemOf } from '@/lib/problem'

import { Group } from './appearance'
import { type EditBlock, editBlocks, localizedOf, localizedValue, moved, savedBlocks } from './appearance-form'
import { equal, LOCALES } from './settings-form'

const MAX_BUTTONS = 6
/** The "theme default" choice (Select cannot hold an empty string). */
const DEFAULT = '__default'

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
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const dirty = !equal(savedBlocks(blocks, types), savedBlocks(initial, types))

  async function save(value: Block[] | null) {
    setPending(true)
    setError(null)
    try {
      onSaved((await updateSettings({ 'home.blocks': value })).data as SettingsList)
      toast.success(value === null ? t('usingThemeNow') : t('saved'))
    } catch (err) {
      // The server checks the blocks against the catalog and says which one is wrong.
      const detail = problemOf(err).fields['home.blocks']
      setError(detail ? t('invalid', { detail }) : t('failed'))
    } finally {
      setPending(false)
    }
  }

  const name = (type: string) => (tb.has(`${type}.name`) ? tb(`${type}.name`) : type)
  const update = (i: number, b: EditBlock) => setBlocks((list) => list.map((x, j) => (j === i ? b : x)))
  const add = (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button variant="outline" size="sm" />}>
        <Plus />
        {t('add')}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-48">
        {types.map((x) => (
          <DropdownMenuItem key={x.type} onClick={() => setBlocks((list) => [...list, { type: x.type, props: {} }])}>
            {name(x.type)}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
  return (
    <Group
      title={t('title')}
      intro={`${t('intro')} ${stored ? t('custom') : t('usingTheme', { theme: theme.name[locale] ?? theme.name.en ?? theme.id })}`}
      action={
        stored && (
          <Button variant="ghost" size="sm" disabled={pending} onClick={() => void save(null)}>
            {t('useTheme')}
          </Button>
        )
      }
    >
      <div className="flex flex-col">
        <ol className="flex flex-col divide-y rounded-xl border bg-card">
          {blocks.length === 0 && <li className="p-5 text-sm text-muted-foreground">{t('empty')}</li>}
          {blocks.map((b, i) => (
            <BlockCard
              key={i}
              index={i}
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
          <li className="flex justify-end p-3">{add}</li>
        </ol>
        {error && (
          <div className="mt-4">
            <FormError message={error} />
          </div>
        )}
        <SaveBar count={dirty ? 1 : 0} saving={pending} onSave={() => void save(savedBlocks(blocks, types))} onDiscard={() => setBlocks(initial)} />
      </div>
    </Group>
  )
}

function BlockCard({
  index,
  block,
  type,
  title,
  first,
  last,
  onChange,
  onMove,
  onRemove,
}: {
  index: number
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
  const editable = (type?.props.length ?? 0) > 0
  return (
    <Collapsible render={<li />} className="group/block flex flex-col">
      <div className="flex items-center gap-2 py-2 pr-2 pl-3">
        <span className="w-6 shrink-0 text-center font-mono text-caption text-faint-foreground">{String(index + 1).padStart(2, '0')}</span>
        <CollapsibleTrigger
          disabled={!editable}
          className="flex min-w-0 flex-1 items-center gap-2 rounded-md px-1 py-1.5 text-left outline-none focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-default"
        >
          {editable && <ChevronRight className="size-4 shrink-0 text-muted-foreground transition-transform duration-150 group-data-[open]/block:rotate-90" aria-hidden />}
          <span className="truncate text-sm font-medium">{title}</span>
          <code className="truncate font-mono text-caption text-faint-foreground max-sm:hidden">{block.type}</code>
        </CollapsibleTrigger>
        {!type && <span className="text-caption text-destructive">{t('unknownType')}</span>}
        <div className="flex shrink-0 gap-0.5">
          <Button variant="ghost" size="icon-sm" aria-label={t('moveUp')} title={t('moveUp')} disabled={first} onClick={() => onMove(-1)}>
            <ArrowUp />
          </Button>
          <Button variant="ghost" size="icon-sm" aria-label={t('moveDown')} title={t('moveDown')} disabled={last} onClick={() => onMove(1)}>
            <ArrowDown />
          </Button>
          <Button variant="ghost" size="icon-sm" aria-label={t('remove')} title={t('remove')} onClick={onRemove}>
            <Trash2 />
          </Button>
        </div>
      </div>
      {editable && (
        <CollapsibleContent className="flex flex-col gap-4 border-t bg-muted/30 px-5 py-4 sm:pl-12">
          {tb.has(`${block.type}.help`) && <p className="text-caption text-muted-foreground">{tb(`${block.type}.help`)}</p>}
          {type?.props.map((p) => (
            <PropField key={p.name} prop={p} value={block.props[p.name]} onChange={(v) => set(p.name, v)} />
          ))}
        </CollapsibleContent>
      )}
    </Collapsible>
  )
}

/** One property, edited by its kind (internal/modules/theme PropType). */
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
    case 'choice': {
      const items = [{ value: DEFAULT, label: t('home.default') }, ...(p.options ?? []).map((o) => ({ value: o, label: option(o) }))]
      control = (
        <Select items={items} value={typeof value === 'string' && value ? value : DEFAULT} onValueChange={(v) => onChange(!v || v === DEFAULT ? undefined : String(v))}>
          <SelectTrigger aria-label={label} className="w-full sm:w-56">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {items.map((i) => (
              <SelectItem key={i.value} value={i.value}>
                {i.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )
      break
    }
    case 'buttons':
      control = <ButtonsInput value={value} onChange={onChange} />
      break
  }
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-caption font-medium">
        {label}
        {p.type === 'number' && <span className="font-normal text-muted-foreground">{` (${p.min}–${p.max})`}</span>}
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
    <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
      {LOCALES.map((l) =>
        multiline ? (
          <Textarea key={l} rows={4} aria-label={t(l)} placeholder={t(l)} value={texts[l]} onChange={(e) => set(l, e.target.value)} />
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
        <div key={i} className="flex flex-col gap-2 rounded-lg border bg-card p-3">
          <LocalizedInput multiline={false} value={item.label} onChange={(label) => set(i, { ...item, label })} />
          <div className="flex gap-2">
            <Input value={item.href ?? ''} placeholder={t('linkHint')} onChange={(e) => set(i, { ...item, href: e.target.value })} />
            <Button variant="ghost" size="icon" aria-label={t('removeButton')} title={t('removeButton')} onClick={() => onChange(items.filter((_, j) => j !== i))}>
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
