'use client'

import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@skinloom/ui'

/** The value of "no filter" (Select cannot hold an empty string). */
export const ANY = 'any'

/**
 * A filter of an admin list: "all" first, then the choices. The value is '' when nothing
 * is picked, so it maps straight onto optional query parameters.
 */
export function FilterSelect({
  label,
  all,
  value,
  onChange,
  options,
  className,
}: {
  label: string
  all: string
  value: string
  onChange: (value: string) => void
  options: { value: string; label: string }[]
  className?: string
}) {
  const items = [{ value: ANY, label: all }, ...options]
  return (
    <Select items={items} value={value || ANY} onValueChange={(v) => onChange(!v || v === ANY ? '' : String(v))}>
      <SelectTrigger aria-label={label} className={className ?? 'w-36'}>
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
}
