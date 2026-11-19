import { PluginPageHost } from '@/components/plugin-page-host'

export default async function AdminPluginPage({
  params,
}: {
  params: Promise<{ id: string; slug?: string[] }>
}) {
  const { id, slug } = await params
  return <PluginPageHost pluginId={id} slug={slug} area="admin" />
}
