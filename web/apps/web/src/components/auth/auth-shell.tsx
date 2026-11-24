import { listTextures, type TexturePage } from '@skinloom/api-client'
import { TexturePreview } from '@skinloom/ui'

import { ThemeOverride } from '@/components/theme-override'
import { getBootstrap } from '@/lib/bootstrap'
import { layoutOf } from '@/lib/theme'

/**
 * The frame of the sign-in, registration and recovery pages, in the theme's auth layout
 * (design.md §9.2): "card" centres the form in a 400px column; "split" puts it beside a
 * panel (.sl-auth-panel) with the site's name, its description and a few of its latest
 * skins on the pixel backdrop. Themes may restyle the panel.
 */
export async function AuthShell({ children }: { children: React.ReactNode }) {
  const boot = await getBootstrap()
  const authLayout = layoutOf(boot.theme.layout, 'auth')
  if (authLayout === 'card') {
    const content = (
      <div data-sl-area="auth" className="mx-auto flex w-full max-w-[25rem] flex-col gap-6 px-4 py-12 sm:py-16">
        {children}
      </div>
    )
    return <ThemeOverride name="auth.background" props={{ layout: authLayout, siteName: boot.site.name }} fallback={content} />
  }
  // The latest public skins, the same for every visitor (no session forwarded).
  const skins = await listTextures({ sort: 'time', category: 'skin', limit: 3 }).then(
    (r) => (r.data as TexturePage).textures,
    () => [],
  )
  const content = (
    <div data-sl-area="auth" className="mx-auto grid w-full max-w-5xl gap-8 px-4 py-12 sm:px-6 md:grid-cols-2 md:py-16">
      <aside className="sl-auth-panel sl-pixel-backdrop relative isolate hidden flex-col justify-between gap-8 overflow-hidden rounded-2xl border p-8 md:flex">
        <div className="flex flex-col gap-2">
          <p className="text-2xl font-medium">{boot.site.name}</p>
          {boot.site.description && <p className="text-body text-muted-foreground">{boot.site.description}</p>}
        </div>
        {skins.length > 0 && (
          // Each preview shrinks with the panel (it is narrow beside the form on tablets).
          <div className="flex items-end justify-center gap-3">
            {skins.map((x) => (
              <div key={x.id} className="flex min-w-0 flex-1 justify-center">
                <TexturePreview texture={{ id: x.id }} type={x.type} height={120} alt={x.name} className="h-auto w-full max-w-[8.5rem]" />
              </div>
            ))}
          </div>
        )}
      </aside>
      <div className="flex flex-col gap-6 md:py-6">{children}</div>
    </div>
  )
  return <ThemeOverride name="auth.background" props={{ layout: authLayout, siteName: boot.site.name }} fallback={content} />
}
