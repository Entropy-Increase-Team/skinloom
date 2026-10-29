'use client'

import { useFormatter, useTranslations } from 'next-intl'
import { useState } from 'react'

const W = 600
const H = 150
const LEFT = 32 // y-axis labels
const BOTTOM = 20 // x-axis labels
const TOP = 8
const PLOT_W = W - LEFT
const PLOT_H = H - TOP - BOTTOM

/** The smallest clean number (1, 2 or 5 times a power of ten) at or above n. */
export function niceMax(n: number): number {
  if (n <= 1) return 1
  const p = 10 ** Math.floor(Math.log10(n))
  for (const m of [1, 2, 5, 10]) if (m * p >= n) return m * p
  return 10 * p
}

/** A column with a 4px rounded top and a square foot on the baseline. */
function column(x: number, w: number, h: number): string {
  const r = Math.min(4, w / 2, h)
  const y = TOP + PLOT_H - h
  return `M${x},${TOP + PLOT_H}V${y + r}Q${x},${y} ${x + r},${y}H${x + w - r}Q${x + w},${y} ${x + w},${y + r}V${TOP + PLOT_H}Z`
}

/**
 * One count per day as columns (a single series: the title names it, no legend).
 * Hovering or focusing a day shows its value; the table below has every value.
 */
export function DailyChart({ title, days }: { title: string; days: { date: string; value: number }[] }) {
  const t = useTranslations('adminOverview')
  const format = useFormatter()
  const [active, setActive] = useState<number | null>(null)
  const total = days.reduce((sum, d) => sum + d.value, 0)
  const max = niceMax(Math.max(0, ...days.map((d) => d.value)))
  const slot = PLOT_W / Math.max(1, days.length)
  const barW = Math.min(24, slot - 2) // 2px of surface between neighbours
  const label = (date: string) => date.slice(5) // MM-DD
  const ticks = days.length > 2 ? [0, Math.floor((days.length - 1) / 2), days.length - 1] : days.map((_, i) => i)
  const shown = active === null ? null : days[active]
  const at = active === null ? 0 : ((LEFT + active * slot + slot / 2) / W) * 100 // tooltip anchor, % of the width

  return (
    <figure className="flex flex-col gap-2">
      <figcaption className="flex items-baseline justify-between gap-2">
        <span className="text-sm font-medium">{title}</span>
        <span className="text-xs text-muted-foreground">{t('total', { count: total })}</span>
      </figcaption>
      <div className="relative">
        <svg viewBox={`0 0 ${W} ${H}`} className="h-auto w-full" role="img" aria-label={`${title}: ${t('total', { count: total })}`}>
          <line x1={LEFT} x2={W} y1={TOP} y2={TOP} stroke="var(--border)" strokeWidth={1} />
          <line x1={LEFT} x2={W} y1={TOP + PLOT_H} y2={TOP + PLOT_H} stroke="var(--border)" strokeWidth={1} />
          <text x={LEFT - 6} y={TOP + 4} textAnchor="end" className="fill-muted-foreground text-[11px]">
            {format.number(max, { notation: 'compact' })}
          </text>
          <text x={LEFT - 6} y={TOP + PLOT_H + 4} textAnchor="end" className="fill-muted-foreground text-[11px]">
            0
          </text>
          {days.map((d, i) => {
            const x = LEFT + i * slot + (slot - barW) / 2
            const h = (d.value / max) * PLOT_H
            return (
              <g key={d.date}>
                {h > 0 && <path d={column(x, barW, h)} fill="var(--chart-1)" opacity={active === null || active === i ? 1 : 0.55} />}
                {/* The whole slot is the hit target, taller and wider than the column. */}
                <rect
                  x={LEFT + i * slot}
                  y={TOP}
                  width={slot}
                  height={PLOT_H}
                  fill="transparent"
                  tabIndex={0}
                  aria-label={`${d.date}: ${d.value}`}
                  onPointerEnter={() => setActive(i)}
                  onPointerLeave={() => setActive(null)}
                  onFocus={() => setActive(i)}
                  onBlur={() => setActive(null)}
                  className="outline-none focus-visible:stroke-ring"
                />
              </g>
            )
          })}
          {ticks.map((i) => (
            <text
              key={i}
              x={LEFT + i * slot + slot / 2}
              y={H - 4}
              textAnchor={i === 0 ? 'start' : i === days.length - 1 ? 'end' : 'middle'}
              className="fill-muted-foreground text-[11px]"
            >
              {label(days[i].date)}
            </text>
          ))}
        </svg>
        {shown && active !== null && (
          <div
            role="status"
            className={`pointer-events-none absolute top-0 rounded-md border bg-popover px-2 py-1 text-xs shadow-sm ${
              // Near an edge the tooltip opens inwards, so it never hangs off the card.
              at > 85 ? '-translate-x-full' : at < 15 ? '' : '-translate-x-1/2'
            }`}
            style={{ left: `${at}%` }}
          >
            <strong className="block text-sm tabular-nums">{format.number(shown.value)}</strong>
            <span className="text-muted-foreground">{shown.date}</span>
          </div>
        )}
      </div>
      <details className="text-xs">
        <summary className="cursor-pointer text-muted-foreground">{t('table')}</summary>
        <table className="mt-2 w-full">
          <tbody className="divide-y">
            {days.map((d) => (
              <tr key={d.date}>
                <td className="py-1 text-muted-foreground">{d.date}</td>
                <td className="py-1 text-right tabular-nums">{d.value}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </details>
    </figure>
  )
}
