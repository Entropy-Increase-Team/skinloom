/**
 * A named extension point (docs/design/05 §5.3). Plugin components registered for the
 * slot are mounted here on the client once the plugin runtime lands (P2). Until then a
 * slot renders its fallback children, so pages can already declare their slots.
 */
export function Slot({ name, children }: { name: string; children?: React.ReactNode }) {
  return (
    <div data-sl-slot={name} className="contents">
      {children}
    </div>
  )
}
