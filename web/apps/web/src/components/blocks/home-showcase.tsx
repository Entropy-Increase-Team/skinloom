'use client'

import { type CarouselSkin, SkinCarousel } from '@skinloom/ui'
import Link from 'next/link'
import type * as React from 'react'

/**
 * The hero's showcase on the client. A server component cannot hand Next's Link to a
 * client component as a prop, so this wrapper supplies it.
 */
export function HomeShowcase({ items, label, className }: { items: CarouselSkin[]; label: React.ReactNode; className?: string }) {
  return <SkinCarousel items={items} label={label} linkAs={Link} className={className} />
}
