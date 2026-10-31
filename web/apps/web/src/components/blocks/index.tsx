import type { Block } from '@skinloom/api-client'

import { AnnouncementBlock } from './announcement'
import { ButtonsBlock, ImageBlock, MarkdownBlock } from './content'
import { FeaturesBlock } from './features'
import { HeroBlock } from './hero'
import { LauncherGuideBlock } from './launcher-guide'
import type { Props } from './props'
import { StatsBlock } from './stats'
import { LatestTexturesBlock, PopularTexturesBlock } from './textures'

/**
 * Built-in block types, rendered on the server (SEO-friendly); the same list as the
 * block catalog in internal/theme/blocks.go. Themes compose pages from these in
 * plugin.json → theme.home; admins override via the home.blocks setting.
 * Plugin-provided block types render on the client once the plugin runtime lands (P2).
 */
const builtin: Record<string, (p: { props: Props }) => Promise<React.ReactNode> | React.ReactNode> = {
  hero: HeroBlock,
  announcement: AnnouncementBlock,
  features: FeaturesBlock,
  stats: StatsBlock,
  'latest-textures': LatestTexturesBlock,
  'popular-textures': PopularTexturesBlock,
  markdown: MarkdownBlock,
  image: ImageBlock,
  buttons: ButtonsBlock,
  'launcher-guide': LauncherGuideBlock,
}

export function Blocks({ blocks }: { blocks: Block[] }) {
  return (
    <>
      {blocks.map((b, i) => {
        const Component = builtin[b.type]
        if (!Component) return null // unknown or plugin block: skipped until P2
        return <Component key={`${b.type}-${i}`} props={(b.props ?? {}) as Props} />
      })}
    </>
  )
}
