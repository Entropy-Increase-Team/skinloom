// The design docs and ADRs are separate docs instances, and Docusaurus resolves .md
// links only within one instance. Links between them are written as repository paths
// (../adr/0012-x.md) so they work on GitHub; on the site they become site paths
// (/adr/0012-x), which <Link> prefixes with the base URL.
type MdNode = { type: string; url?: string; children?: MdNode[] }

const crossInstance = /^\.\.\/(adr|design)\/([^#]*?)(#.*)?$/

export default function crossInstanceLinks() {
  return (tree: MdNode) => {
    const walk = (node: MdNode) => {
      const m = node.type === 'link' && node.url ? crossInstance.exec(node.url) : null
      if (m) {
        const [, area, file, hash = ''] = m
        const slug = file.replace(/(^|\/)README\.md$/, '').replace(/\.md$/, '')
        node.url = `/${area}${slug ? `/${slug}` : ''}${hash}`
      }
      node.children?.forEach(walk)
    }
    walk(tree)
  }
}
