import { Blocks } from '@/components/blocks'
import { getBootstrap } from '@/lib/bootstrap'

export default async function HomePage() {
  const boot = await getBootstrap()
  return <Blocks blocks={boot.theme.home} />
}
