/** Visually hidden link that reveals on keyboard focus to jump past the header navigation. */
export function SkipLink({ label }: { label: string }) {
  return (
    <a
      href="#main"
      className="sr-only focus:not-sr-only focus:fixed focus:top-3 focus:left-3 focus:z-50 focus:rounded-md focus:bg-popover focus:px-3 focus:py-2 focus:text-sm focus:font-medium focus:text-popover-foreground focus:shadow-(--shadow-popover) focus:ring-2 focus:ring-ring focus:outline-none"
    >
      {label}
    </a>
  )
}
