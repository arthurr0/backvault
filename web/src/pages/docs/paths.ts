export const DOCS_INDEX_PAGE = 'README.md'
export const DOCS_GITHUB_BASE = 'https://github.com/arthurr0/backvault/blob/master/docs'
export const DOCS_ASSET_BASE = '/api/v1/docs/assets'

function normalize(value: string): string {
  const out: string[] = []
  for (const part of value.split('/')) {
    if (part === '' || part === '.') continue
    if (part === '..') {
      out.pop()
      continue
    }
    out.push(part)
  }
  return out.join('/')
}

function directoryOf(docPath: string): string {
  const at = docPath.lastIndexOf('/')
  return at < 0 ? '' : docPath.slice(0, at)
}

export function routeForDoc(docPath: string): string {
  const base = docPath.replace(/\.md$/i, '')
  if (base === 'README') return '/docs'
  if (base.endsWith('/README')) return `/docs/${base.slice(0, -'/README'.length)}`
  return `/docs/${base}`
}

export function docPathForRoute(slug: string, known: Set<string>): string {
  const clean = normalize(slug)
  if (!clean) return DOCS_INDEX_PAGE
  const candidates = [`${clean}.md`, `${clean}/README.md`, clean]
  for (const candidate of candidates) {
    if (known.has(candidate)) return candidate
  }
  return `${clean}.md`
}

export interface ResolvedDocLink {
  path: string
  hash: string
}

export function resolveDocLink(fromPath: string, href: string): ResolvedDocLink | null {
  const [target, anchor] = href.split('#')
  if (!target.toLowerCase().endsWith('.md')) return null
  const path = normalize(`${directoryOf(fromPath)}/${target}`)
  if (!path) return null
  return { path, hash: anchor ? `#${anchor}` : '' }
}

export function resolveAssetUrl(fromPath: string, src: string): string {
  if (/^(https?:)?\/\//i.test(src) || src.startsWith('data:')) return src
  const path = normalize(`${directoryOf(fromPath)}/${src}`)
  return `${DOCS_ASSET_BASE}/${path}`
}

export function githubUrlFor(docPath: string): string {
  return `${DOCS_GITHUB_BASE}/${docPath}`
}
