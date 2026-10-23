import type { SidebarsConfig } from '@docusaurus/plugin-content-docs'

const sidebars: SidebarsConfig = {
  guide: [
    'intro',
    { type: 'category', label: '开始使用', collapsed: false, items: ['getting-started/quick-start', 'getting-started/configuration'] },
    {
      type: 'category',
      label: '部署',
      items: ['deployment/overview', 'deployment/compose', 'deployment/docker', 'deployment/kubernetes', 'deployment/binary', 'deployment/reverse-proxy'],
    },
    'acknowledgements',
  ],
  dev: ['development/setup', 'development/architecture', 'development/docs-and-logs'],
  plugins: ['plugins/overview'],
}

export default sidebars
