import type * as Preset from '@docusaurus/preset-classic'
import type { Config } from '@docusaurus/types'
import { themes as prismThemes } from 'prism-react-renderer'

const repo = 'https://github.com/Entropy-Increase-Team/skinloom'

const config: Config = {
  title: '云织 SkinLoom',
  tagline: '开源、自托管的 Minecraft 皮肤站',
  favicon: 'img/logo.svg',
  url: 'https://skinloom.cn',
  baseUrl: '/',
  organizationName: 'Entropy-Increase-Team',
  projectName: 'skinloom',
  trailingSlash: false,

  // Design docs and ADRs contain literal {placeholders} and <tags>; parse plain .md files
  // as CommonMark instead of MDX so they render as written.
  markdown: { format: 'detect' },
  onBrokenLinks: 'warn',

  i18n: { defaultLocale: 'zh-Hans', locales: ['zh-Hans'] },

  presets: [
    [
      'classic',
      {
        docs: {
          path: 'docs',
          routeBasePath: 'docs',
          sidebarPath: './sidebars.ts',
          editUrl: `${repo}/edit/main/website/`,
        },
        blog: {
          path: 'devlog',
          routeBasePath: 'devlog',
          blogTitle: '开发日志',
          blogDescription: 'SkinLoom 的开发过程、决策与踩坑记录',
          blogSidebarTitle: '全部日志',
          blogSidebarCount: 'ALL',
          showReadingTime: true,
          onUntruncatedBlogPosts: 'ignore',
          editUrl: `${repo}/edit/main/website/`,
        },
        theme: { customCss: './src/css/custom.css' },
      } satisfies Preset.Options,
    ],
  ],

  plugins: [
    [
      '@docusaurus/plugin-content-docs',
      {
        id: 'design',
        path: '../docs/design',
        routeBasePath: 'design',
        sidebarPath: false,
        numberPrefixParser: false, // keep 04-rewrite-plan as the URL, matching the file name
        editUrl: `${repo}/edit/main/`,
      },
    ],
    [
      '@docusaurus/plugin-content-docs',
      {
        id: 'adr',
        path: '../docs/adr',
        routeBasePath: 'adr',
        sidebarPath: false,
        numberPrefixParser: false,
        editUrl: `${repo}/edit/main/`,
      },
    ],
  ],

  themeConfig: {
    colorMode: { respectPrefersColorScheme: true },
    navbar: {
      title: '云织 SkinLoom',
      logo: { alt: 'SkinLoom', src: 'img/logo.svg' },
      items: [
        { type: 'docSidebar', sidebarId: 'guide', position: 'left', label: '使用指南' },
        { type: 'docSidebar', sidebarId: 'dev', position: 'left', label: '开发' },
        { type: 'docSidebar', sidebarId: 'plugins', position: 'left', label: '插件开发' },
        { to: '/design/', label: '设计文档', position: 'left' },
        { to: '/adr/', label: 'ADR', position: 'left' },
        { to: '/devlog', label: '开发日志', position: 'left' },
        { href: repo, label: 'GitHub', position: 'right' },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: '文档',
          items: [
            { label: '快速开始', to: '/docs/getting-started/quick-start' },
            { label: '插件开发', to: '/docs/plugins/overview' },
            { label: '更新日志', href: `${repo}/blob/main/CHANGELOG.md` },
          ],
        },
        {
          title: '社区',
          items: [
            { label: 'GitHub', href: repo },
            { label: '参与贡献', href: `${repo}/blob/main/CONTRIBUTING.md` },
          ],
        },
        {
          title: '鸣谢',
          items: [
            { label: 'Blessing Skin', href: 'https://github.com/bs-community/blessing-skin-server' },
            { label: 'skinview3d', href: 'https://github.com/bs-community/skinview3d' },
            { label: 'authlib-injector', href: 'https://github.com/yushijinhun/authlib-injector' },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} SkinLoom contributors · MIT · 与 Mojang Studios、Microsoft 无关`,
    },
    prism: { theme: prismThemes.github, darkTheme: prismThemes.dracula, additionalLanguages: ['bash', 'toml', 'go', 'json'] },
  } satisfies Preset.ThemeConfig,
}

export default config
