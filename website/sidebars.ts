import type { SidebarsConfig } from '@docusaurus/plugin-content-docs'

// One sidebar per audience: players, site owners, developers.
const sidebars: SidebarsConfig = {
  guide: [
    'intro',
    {
      type: 'category',
      label: '玩家指南',
      collapsed: false,
      items: ['players/account', 'players/textures', 'players/characters', 'players/in-game'],
    },
    {
      type: 'category',
      label: '站长指南',
      collapsed: false,
      items: [
        'deployment/quick-start',
        {
          type: 'category',
          label: '部署',
          items: ['deployment/overview', 'deployment/compose', 'deployment/docker', 'deployment/kubernetes', 'deployment/binary', 'deployment/reverse-proxy'],
        },
        'admin/configuration',
        'admin/settings',
        'admin/accounts',
        'admin/management',
        'admin/appearance',
        'admin/social-login',
        'admin/game',
        'deployment/backup-and-cdn',
        'deployment/from-blessing-skin',
      ],
    },
    'acknowledgements',
  ],
  dev: ['development/setup', 'development/architecture', 'development/api', 'development/docs-and-logs', 'plugins/overview'],
}

export default sidebars
