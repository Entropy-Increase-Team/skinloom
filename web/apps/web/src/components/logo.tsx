import { useId } from 'react'

/** SkinLoom mark: airy 2x2 interlocking warp and weft threads. */
export function Logo({ className }: { className?: string }) {
  const id = useId()
  const warpLeft = `${id}-wl`
  const warpRight = `${id}-wr`
  const weftTop = `${id}-wt`
  const weftBottom = `${id}-wb`

  return (
    <svg viewBox="0 0 32 32" className={className} aria-hidden>
      <defs>
        <clipPath id={warpLeft}>
          <rect x="8" y="4" width="5" height="24" rx="1.5" />
        </clipPath>
        <clipPath id={warpRight}>
          <rect x="19" y="4" width="5" height="24" rx="1.5" />
        </clipPath>
        <clipPath id={weftTop}>
          <rect x="4" y="8" width="24" height="5" rx="1.5" />
        </clipPath>
        <clipPath id={weftBottom}>
          <rect x="4" y="19" width="24" height="5" rx="1.5" />
        </clipPath>
      </defs>

      {/* Vertical warp bands (Indigo primary, adapts cleanly to light & dark) */}
      <rect x="8" y="4" width="5" height="24" rx="1.5" fill="var(--primary)" />
      <rect x="19" y="4" width="5" height="24" rx="1.5" fill="var(--primary)" />

      {/* Subtle downward directional shadows onto vertical warp */}
      <rect x="19" y="13" width="5" height="1.25" fill="#0c0e12" opacity="0.18" clipPath={`url(#${warpRight})`} />
      <rect x="8" y="24" width="5" height="1.25" fill="#0c0e12" opacity="0.18" clipPath={`url(#${warpLeft})`} />

      {/* Horizontal weft bands (Gold highlight) */}
      <rect x="4" y="8" width="24" height="5" rx="1.5" fill="var(--highlight)" />
      <rect x="4" y="19" width="24" height="5" rx="1.5" fill="var(--highlight)" />

      {/* Subtle rightward directional shadows onto horizontal weft */}
      <rect x="13" y="8" width="1.25" height="5" fill="#0c0e12" opacity="0.18" clipPath={`url(#${weftTop})`} />
      <rect x="24" y="19" width="1.25" height="5" fill="#0c0e12" opacity="0.18" clipPath={`url(#${weftBottom})`} />

      {/* Over-weave segments (TL & BR) */}
      <rect x="8" y="4" width="5" height="11" rx="1.5" fill="var(--primary)" />
      <rect x="19" y="17" width="5" height="11" rx="1.5" fill="var(--primary)" />
    </svg>
  )
}
