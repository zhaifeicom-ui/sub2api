import { describe, expect, it } from 'vitest'
import { qualityPreviewDocument } from './preview'

describe('quality artifact isolation', () => {
  it('preserves generated body presentation without copying event handlers', () => {
    const source = '<html><body class="scene" style="margin:0;background:#e7f3ee" onload="alert(1)"><svg></svg></body></html>'
    const doc = new DOMParser().parseFromString(qualityPreviewDocument(source), 'text/html')
    expect(doc.body.className).toBe('scene')
    expect(doc.body.style.margin).toBe('0px')
    expect(doc.body.style.backgroundColor).toBe('rgb(231, 243, 238)')
    expect(doc.body.hasAttribute('onload')).toBe(false)
    expect(doc.head.firstElementChild?.getAttribute('content')).toContain("connect-src 'none'")
  })
  it('prepends restrictive CSP and removes active external resources and refresh', () => {
    const source = `<html><head><meta http-equiv="refresh" content="0;url=https://evil.test"><base href="https://evil.test/"><script src="https://evil.test/a.js"></script><link rel="stylesheet" href="https://evil.test/a.css"></head><body><iframe src="https://evil.test"></iframe><form action="https://evil.test"></form><a href="https://evil.test">link</a><svg><circle /></svg><script>requestAnimationFrame(()=>{})</script></body></html>`
    const doc = new DOMParser().parseFromString(qualityPreviewDocument(source), 'text/html')
    expect(doc.head.firstElementChild?.getAttribute('http-equiv')).toBe('Content-Security-Policy')
    expect(doc.head.firstElementChild?.getAttribute('content')).toContain("connect-src 'none'")
    expect(doc.querySelector('iframe,form,base,script[src],link,meta[http-equiv="refresh"]')).toBeNull()
    expect(doc.querySelector('a')?.hasAttribute('href')).toBe(false)
    expect(doc.querySelector('svg circle')).not.toBeNull()
    expect(doc.querySelector('script')?.textContent).toContain('requestAnimationFrame')
  })
  it('wraps standalone SVG in an isolated HTML document', () => {
    expect(qualityPreviewDocument('<svg><circle/></svg>')).toContain('<svg>')
    expect(qualityPreviewDocument('<svg><circle/></svg>')).toContain("default-src 'none'")
  })
})
