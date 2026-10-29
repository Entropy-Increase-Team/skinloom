'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'

export function AdminNav({ title, items }: { title: string; items: { href: string; label: string; exact?: boolean }[] }) {
  const pathname = usePathname()
  return (
    <nav className="flex flex-wrap gap-x-4 gap-y-2 border-b pb-3 text-sm">
      <span className="font-semibold">{title}</span>
      {items.map((n) => {
        const active = pathname === n.href || (!n.exact && pathname.startsWith(`${n.href}/`))
        return (
          <Link
            key={n.href}
            href={n.href}
            aria-current={active ? 'page' : undefined}
            className={active ? 'font-medium text-foreground' : 'text-muted-foreground hover:text-foreground'}
          >
            {n.label}
          </Link>
        )
      })}
    </nav>
  )
}
