import type { UserBadge } from '@skinloom/api-client'
import { Badge, menuIcons } from '@skinloom/ui'

type BadgeVariant = 'default' | 'secondary' | 'destructive' | 'success' | 'warning' | 'highlight' | 'outline'

export function badgeVariant(color?: string): BadgeVariant {
  switch (color?.trim().toLowerCase()) {
    case 'primary':
    case 'default':
      return 'default'
    case 'highlight':
    case 'gold':
      return 'highlight'
    case 'success':
    case 'green':
      return 'success'
    case 'warning':
    case 'amber':
      return 'warning'
    case 'destructive':
    case 'danger':
    case 'red':
      return 'destructive'
    case 'outline':
      return 'outline'
    default:
      return 'secondary'
  }
}

/**
 * Renders plugin-provided user badges (from the `user.badges` filter hook) using
 * semantic design tokens so every active theme styles them consistently.
 */
export function UserBadges({ badges }: { badges?: UserBadge[] | null }) {
  if (!badges || badges.length === 0) return null
  return (
    <span className="inline-flex flex-wrap items-center gap-1">
      {badges.map((b, i) => {
        if (!b.text) return null
        const Icon = b.icon ? menuIcons[b.icon] : undefined
        return (
          <Badge key={`${b.text}-${i}`} variant={badgeVariant(b.color)} className="h-4 px-1.5 text-[0.6875rem]">
            {Icon && <Icon aria-hidden />}
            <span className="max-w-32 truncate">{b.text}</span>
          </Badge>
        )
      })}
    </span>
  )
}
