import { PluginPageHost } from '@/components/plugin-page-host'

export default async function ExplorePluginPage({
  params,
}: {
  params: Promise<{ id: string; slug?: string[] }>
}) {
  const { id, slug } = await params
  return (
    <div className="mx-auto w-full max-w-[75rem] px-4 py-8 sm:px-6 lg:px-8">
      <PluginPageHost pluginId={id} slug={slug} area="explore" />
    </div>
  )
}
