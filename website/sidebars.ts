import type { SidebarsConfig } from '@docusaurus/plugin-content-docs'

const sidebars: SidebarsConfig = {
  guide: [
    'intro',
    { type: 'category', label: '开始使用', collapsed: false, items: ['getting-started/quick-start', 'getting-started/configuration'] },
    { type: 'category', label: '部署', items: ['deployment/overview'] },
    'acknowledgements',
  ],
  dev: ['development/setup', 'development/architecture', 'development/docs-and-logs'],
  plugins: ['plugins/overview'],
}

export default sidebars
