'use client'

import { DemoSkinShowcase as UIDemoSkinShowcase, type DemoSkinShowcaseProps } from '@skinloom/ui'
import Link from 'next/link'

/**
 * Client wrapper supplying Next's Link to @skinloom/ui's DemoSkinShowcase.
 */
export function DemoSkinShowcase(props: Omit<DemoSkinShowcaseProps, 'linkAs'>) {
  return <UIDemoSkinShowcase {...props} linkAs={Link} />
}
