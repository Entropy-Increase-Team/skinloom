'use client'

import { Badge } from '@skinloom/ui'
import { useTranslations } from 'next-intl'

/** Message key of a scope name: "Player.ReadWrite" → "Player_ReadWrite". */
export const scopeKey = (name: string) => name.replace('.', '_')

/** What each scope allows, in words. */
export function ScopeList({ scopes }: { scopes: string[] }) {
  const t = useTranslations('oauth.scopes')
  return (
    <ul className="flex flex-col gap-2">
      {scopes.map((s) => (
        <li key={s} className="flex flex-wrap items-center gap-2 text-sm">
          <Badge variant="secondary" className="font-mono">
            {s}
          </Badge>
          <span>{t.has(scopeKey(s)) ? t(scopeKey(s) as 'User_Read') : s}</span>
        </li>
      ))}
    </ul>
  )
}
