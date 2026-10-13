// This must only be used in an opaque-origin sandbox iframe, never v-html.
export function qualityPreviewDocument(source: string): string {
  // An unattached template is inert, including resource loading; DOMParser
  // documents may fetch images/frames before sanitization in some browsers.
  const template = document.createElement('template')
  template.innerHTML = source
  const doc = template.content
  // The policy is ours; generated policies/refresh tags cannot precede or replace it.
  doc.querySelectorAll('meta,base,iframe,frame,object,embed,form').forEach(el => el.remove())
  doc.querySelectorAll('script[src],link').forEach(el => el.remove())
  doc.querySelectorAll('a,area').forEach(el => { el.removeAttribute('href'); el.removeAttribute('target') })
  const csp = document.createElement('meta')
  csp.httpEquiv = 'Content-Security-Policy'
  csp.content = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src 'none'; connect-src 'none'; media-src 'none'; frame-src 'none'; worker-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'"
  // Fragment parsing discards the outer body tag. Preserve its presentation
  // attributes using another inert template, without activating resources.
  const bodyMatch = source.match(/<body\b(?:[^>"']|"[^"]*"|'[^']*')*>/i)
  let bodyTag = '<body>'
  if (bodyMatch) {
    const holder = document.createElement('template')
    holder.innerHTML = bodyMatch[0].replace(/^<body/i, '<div') + '</div>'
    const element = holder.content.firstElementChild
    if (element) {
      const allowed = new Set(['class', 'style', 'dir', 'lang', 'id'])
      for (const attribute of Array.from(element.attributes)) {
        if (!allowed.has(attribute.name)) element.removeAttribute(attribute.name)
      }
      bodyTag = element.outerHTML.replace(/<\/div>$/, '').replace(/^<div/, '<body')
    }
  }
  return '<!doctype html>\n<html><head>' + csp.outerHTML + '</head>' + bodyTag + template.innerHTML + '</body></html>'
}
