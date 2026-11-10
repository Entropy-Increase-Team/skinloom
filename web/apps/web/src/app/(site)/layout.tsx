import { SiteFooter } from '@/components/shell/site-footer'
import { SiteHeader } from '@/components/shell/site-header'

/** Public pages: the header in the page column, the content, the footer (design.md §9.2). */
export default function SiteLayout({ children }: { children: React.ReactNode }) {
  return (
    <div data-sl-area="public" className="flex min-h-svh flex-col">
      <SiteHeader />
      <main className="flex-1">{children}</main>
      <SiteFooter />
    </div>
  )
}
