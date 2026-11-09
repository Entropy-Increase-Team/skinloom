import Link from '@docusaurus/Link'
import useDocusaurusContext from '@docusaurus/useDocusaurusContext'
import Layout from '@theme/Layout'

export default function Home() {
  const { siteConfig } = useDocusaurusContext()
  return (
    <Layout description={siteConfig.tagline}>
      <main className="container margin-vert--xl">
        <h1 className="hero__title">{siteConfig.title}</h1>
        <p className="hero__subtitle">{siteConfig.tagline}</p>
        <p>兼容 Blessing Skin · authlib-injector 外置登录 · Docker 与发行包一键运行 · MIT</p>
        <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
          <Link className="button button--primary button--lg" to="/docs/deployment/quick-start">搭建皮肤站</Link>
          <Link className="button button--secondary button--lg" to="/docs/players/account">玩家指南</Link>
          <Link className="button button--secondary button--lg" to="/devlog">开发日志</Link>
        </div>
        <p className="margin-top--lg" style={{ opacity: 0.8 }}>
          设计参考并致敬 <a href="https://github.com/bs-community/blessing-skin-server">Blessing Skin</a>。
        </p>
      </main>
    </Layout>
  )
}
