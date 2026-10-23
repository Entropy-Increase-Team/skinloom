/** SkinLoom mark: a pixel head woven from warp and weft threads. */
export function Logo({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" className={className} aria-hidden>
      <rect width="32" height="32" rx="7" fill="var(--primary)" />
      <g fill="var(--primary-foreground)">
        <rect x="8" y="8" width="16" height="4" rx="1" opacity=".55" />
        <rect x="8" y="14" width="4" height="4" rx="1" />
        <rect x="20" y="14" width="4" height="4" rx="1" />
        <rect x="12" y="20" width="8" height="4" rx="1" opacity=".8" />
        <rect x="14" y="6" width="4" height="20" rx="1" opacity=".25" />
      </g>
    </svg>
  )
}
