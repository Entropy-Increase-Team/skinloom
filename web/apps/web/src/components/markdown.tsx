/**
 * Shows Markdown the server has rendered to HTML (raw HTML and script links are
 * removed there, see internal/notify), styled by .sl-markdown.
 */
export function Markdown({ html, className }: { html: string; className?: string }) {
  return <div className={`sl-markdown ${className ?? ''}`} dangerouslySetInnerHTML={{ __html: html }} />
}
