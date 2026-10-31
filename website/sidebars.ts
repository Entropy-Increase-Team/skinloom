import type { SidebarsConfig } from '@docusaurus/plugin-content-docs'

const sidebars: SidebarsConfig = {
  guide: [
    'intro',
    { type: 'category', label: '开始使用', collapsed: false, items: ['getting-started/quick-start', 'getting-started/configuration', 'getting-started/accounts', 'getting-started/textures', 'getting-started/players', 'getting-started/launchers', 'getting-started/open-platform', 'getting-started/social-login', 'getting-started/appearance', 'getting-started/admin-tools'] },
    {
      type: 'category',
      label: '部署',
      items: ['deployment/overview', 'deployment/compose', 'deployment/docker', 'deployment/kubernetes', 'deployment/binary', 'deployment/reverse-proxy', 'deployment/backup-and-cdn'],
    },
    'acknowledgements',
  ],
  dev: ['development/setup', 'development/architecture', 'development/docs-and-logs'],
  plugins: ['plugins/overview'],
}

export default sidebars
