import { getBootstrap } from '@/lib/bootstrap'
import { layoutOf } from '@/lib/theme'

/**
 * The frame of the sign-in, registration and recovery pages, in the theme's auth layout:
 * "card" centers the form; "split" puts it beside a panel with the site's name and
 * description (.sl-auth-panel, which themes may restyle).
 */
export async function AuthShell({ children }: { children: React.ReactNode }) {
  const boot = await getBootstrap()
  if (layoutOf(boot.theme.layout, 'auth') === 'card') {
    return <div className="mx-auto flex max-w-md flex-col gap-6 px-4 py-12">{children}</div>
  }
  return (
    <div className="mx-auto grid max-w-5xl gap-8 px-4 py-12 md:grid-cols-2">
      <aside className="sl-auth-panel relative isolate hidden flex-col justify-end overflow-hidden rounded-xl p-8 text-white md:flex">
        <p className="text-2xl font-semibold">{boot.site.name}</p>
        <p className="mt-2 text-white/85">{boot.site.description}</p>
      </aside>
      <div className="flex flex-col gap-6 md:py-6">{children}</div>
    </div>
  )
}
