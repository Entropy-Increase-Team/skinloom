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
        <p>Go 后端 + Next.js 前端 · 一键运行 · 全链路热加载 · 统一的插件与主题标准 · MIT</p>
        <div style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
          <Link className="button button--primary button--lg" to="/docs/getting-started/quick-start">快速开始</Link>
          <Link className="button button--secondary button--lg" to="/docs/plugins/overview">插件开发</Link>
          <Link className="button button--secondary button--lg" to="/devlog">开发日志</Link>
        </div>
        <p className="margin-top--lg" style={{ opacity: 0.8 }}>
          设计参考并致敬 <a href="https://github.com/bs-community/blessing-skin-server">Blessing Skin</a>。
        </p>
      </main>
    </Layout>
  )
}
