'use client'

import { Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import type * as React from 'react'

import { cn } from '../../lib/utils'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '../ui/dialog'
import { InputGroup, InputGroupAddon, InputGroupInput } from '../ui/input-group'
import { ToggleGroup, ToggleGroupItem } from '../ui/toggle-group'
import { useUiLabels } from './labels'
import { PixelBackdrop } from './pixel-backdrop'
import { type TextureType, TexturePreview } from './texture-preview'

export interface PickableTexture {
  id: number
  name: string
  type: TextureType
}

type Kind = 'all' | 'skin' | 'cape'

/**
 * Choose a texture from a list (the closet) in a dialog: filter by kind, search by name,
 * pick from the dual views (web/design.md §10.3). Replaces dropdowns of texture names.
 */
export function TexturePicker({
  open,
  onOpenChange,
  title,
  description,
  items,
  kind: fixedKind,
  selectedId,
  onPick,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: React.ReactNode
  description?: React.ReactNode
  items: PickableTexture[]
  /** Only this kind, without the filter (e.g. a player's cape). */
  kind?: 'skin' | 'cape'
  selectedId?: number | null
  onPick: (texture: PickableTexture) => void
}) {
  const t = useUiLabels()
  const [kind, setKind] = useState<Kind>(fixedKind ?? 'all')
  const [q, setQ] = useState('')
  const shown = useMemo(() => {
    const k = fixedKind ?? kind
    const needle = q.trim().toLowerCase()
    return items.filter((x) => (k === 'all' || (k === 'cape') === (x.type === 'cape')) && (!needle || x.name.toLowerCase().includes(needle)))
  }, [items, kind, fixedKind, q])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[85dvh] flex-col sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {description && <DialogDescription>{description}</DialogDescription>}
        </DialogHeader>
        <div className="flex flex-wrap items-center gap-2">
          {!fixedKind && (
            <ToggleGroup value={[kind]} onValueChange={(v) => v[0] && setKind(v[0] as Kind)} variant="outline" size="sm">
              <ToggleGroupItem value="all">{t.all}</ToggleGroupItem>
              <ToggleGroupItem value="skin">{t.skins}</ToggleGroupItem>
              <ToggleGroupItem value="cape">{t.capes}</ToggleGroupItem>
            </ToggleGroup>
          )}
          <InputGroup className="min-w-40 flex-1">
            <InputGroupAddon>
              <Search />
            </InputGroupAddon>
            <InputGroupInput value={q} onChange={(e) => setQ(e.target.value)} placeholder={t.search} aria-label={t.search} />
          </InputGroup>
        </div>
        <div className="-mx-1 min-h-0 flex-1 overflow-y-auto px-1 py-1">
          {shown.length === 0 ? (
            <p className="py-12 text-center text-sm text-muted-foreground">{t.noMatch}</p>
          ) : (
            <ul className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              {shown.map((x) => (
                <li key={x.id}>
                  <button
                    type="button"
                    aria-pressed={x.id === selectedId}
                    onClick={() => onPick(x)}
                    className={cn(
                      'flex w-full flex-col gap-1.5 rounded-lg border bg-card p-1 text-left outline-none transition-colors duration-150 hover:bg-muted/40 focus-visible:ring-3 focus-visible:ring-ring/50',
                      x.id === selectedId && 'border-primary ring-1 ring-primary',
                    )}
                  >
                    <PixelBackdrop className="h-28 rounded-md">
                      <TexturePreview texture={{ id: x.id }} type={x.type} height={88} />
                    </PixelBackdrop>
                    <span className="truncate px-1 pb-0.5 text-caption">{x.name}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
