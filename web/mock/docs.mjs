import { readdirSync, readFileSync, statSync } from 'node:fs'
import { extname, join, posix } from 'node:path'

const INDEX_PAGE = 'README.md'
const OTHER = 'Other'
const OVERVIEW = 'Overview'
const DESCRIPTION_LIMIT = 220
const SNIPPET_RADIUS = 60

const LINK = /\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g
const HEADING = /^(#{1,6})\s+(.*?)\s*#*\s*$/
const FENCE = /^\s{0,3}(```|~~~)/

function walk(root, prefix = '') {
  const out = []
  for (const entry of readdirSync(join(root, prefix), { withFileTypes: true })) {
    const rel = prefix ? posix.join(prefix, entry.name) : entry.name
    if (entry.isDirectory()) out.push(...walk(root, rel))
    else if (entry.name.endsWith('.md')) out.push(rel)
  }
  return out
}

function stripInline(value) {
  return value
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/<[^>]+>/g, '')
    .replace(/`/g, '')
    .replace(/\*/g, '')
    .replace(/~~/g, '')
    .trim()
}

function collapse(value) {
  return value.split(/\s+/).filter(Boolean).join(' ')
}

function truncate(value, limit) {
  const clean = collapse(value)
  if (clean.length <= limit) return clean
  let cut = clean.slice(0, limit)
  const space = cut.lastIndexOf(' ')
  if (space > limit / 2) cut = cut.slice(0, space)
  return `${cut.replace(/[ ,.;:]+$/, '')}...`
}

function slug(value) {
  let out = ''
  for (const symbol of value.trim().toLowerCase()) {
    if (symbol === ' ' || symbol === '\t') out += '-'
    else if (symbol === '-' || symbol === '_' || /[\p{L}\p{N}]/u.test(symbol)) out += symbol
  }
  return out
}

function uniqueSlug(seen, value) {
  const base = slug(value)
  if (!seen.has(base)) {
    seen.set(base, 0)
    return base
  }
  let count = seen.get(base)
  for (;;) {
    count += 1
    const candidate = `${base}-${count}`
    if (!seen.has(candidate)) {
      seen.set(base, count)
      seen.set(candidate, 0)
      return candidate
    }
  }
}

function headingOf(line) {
  const match = HEADING.exec(line)
  if (!match) return null
  return { level: match[1].length, text: stripInline(match[2]) }
}

function resolveDocPath(base, target) {
  if (!target || target.startsWith('#')) return ''
  if (/^(https?:|mailto:)/i.test(target)) return ''
  const clean = target.split('#')[0]
  if (!clean.endsWith('.md')) return ''
  const joined = posix.normalize(posix.join(posix.dirname(base || 'x'), clean))
  return joined.startsWith('..') ? '' : joined
}

function parsePage(name, content) {
  const page = {
    path: name,
    title: '',
    section: OTHER,
    description: '',
    headings: [],
    words: content.split(/\s+/).filter(Boolean).length,
    markdown: content,
    plain: '',
    order: 0,
  }
  const seen = new Map()
  const plain = []
  const description = []
  let fence = false
  let collecting = false
  for (const line of content.split('\n')) {
    if (FENCE.test(line)) {
      fence = !fence
      continue
    }
    if (fence) {
      plain.push(line.trim())
      continue
    }
    const heading = headingOf(line)
    if (heading) {
      if (heading.level === 1 && !page.title) {
        page.title = heading.text
        collecting = true
      } else if (heading.level === 2 || heading.level === 3) {
        page.headings.push({ level: heading.level, text: heading.text, id: uniqueSlug(seen, heading.text) })
      }
      if (heading.level > 1 && collecting) {
        if (description.length) page.description = truncate(description.join(' '), DESCRIPTION_LIMIT)
        collecting = false
      }
      plain.push(heading.text)
      continue
    }
    const text = stripInline(line)
    plain.push(text)
    const trimmed = text.trim()
    if (collecting && !page.description) {
      if (!trimmed) {
        if (description.length) {
          page.description = truncate(description.join(' '), DESCRIPTION_LIMIT)
          collecting = false
        }
        continue
      }
      if (/^[|\-*>]/.test(trimmed)) {
        if (description.length) {
          page.description = truncate(description.join(' '), DESCRIPTION_LIMIT)
          collecting = false
        }
        continue
      }
      description.push(trimmed)
    }
  }
  if (!page.description && description.length) {
    page.description = truncate(description.join(' '), DESCRIPTION_LIMIT)
  }
  if (!page.title) page.title = name.split('/').pop().replace(/\.md$/, '')
  page.plain = collapse(plain.join(' '))
  return page
}

function applyOrder(pages, byPath) {
  const sections = [OVERVIEW]
  const placements = new Map()
  let order = 0
  const index = byPath.get(INDEX_PAGE)
  if (index) {
    placements.set(INDEX_PAGE, { section: OVERVIEW, order })
    order += 1
    let current = ''
    for (const line of index.markdown.split('\n')) {
      const heading = headingOf(line)
      if (heading && heading.level === 2) {
        current = heading.text
        continue
      }
      LINK.lastIndex = 0
      let match = LINK.exec(line)
      while (match) {
        const resolved = resolveDocPath('', match[2])
        if (resolved && byPath.has(resolved) && !placements.has(resolved)) {
          const section = current || OTHER
          if (!sections.includes(section)) sections.push(section)
          placements.set(resolved, { section, order })
          order += 1
        }
        match = LINK.exec(line)
      }
    }
  }
  let hasOther = false
  for (const page of pages) {
    const spot = placements.get(page.path)
    if (spot) {
      page.section = spot.section
      page.order = spot.order
      continue
    }
    page.section = OTHER
    page.order = order
    order += 1
    hasOther = true
  }
  if (hasOther && !sections.includes(OTHER)) sections.push(OTHER)
  pages.sort((left, right) => {
    const rank = sections.indexOf(left.section) - sections.indexOf(right.section)
    return rank !== 0 ? rank : left.order - right.order
  })
  return sections.filter((name) => pages.some((page) => page.section === name))
}

function snippetFor(page, needle, tokens) {
  const body = page.plain
  const lower = body.toLowerCase()
  let at = lower.indexOf(needle)
  let width = needle.length
  if (at < 0) {
    for (const token of tokens) {
      const found = lower.indexOf(token)
      if (found >= 0) {
        at = found
        width = token.length
        break
      }
    }
  }
  if (at < 0) return page.description || truncate(body, DESCRIPTION_LIMIT)
  let start = Math.max(0, at - SNIPPET_RADIUS)
  let end = Math.min(body.length, at + width + SNIPPET_RADIUS)
  while (start > 0 && body[start] !== ' ') start -= 1
  while (end < body.length && body[end] !== ' ') end += 1
  let snippet = body.slice(start, end).trim()
  if (start > 0) snippet = `...${snippet}`
  if (end < body.length) snippet = `${snippet}...`
  return snippet
}

export function loadDocs(root) {
  const pages = walk(root).map((name) => parsePage(name, readFileSync(join(root, name), 'utf8')))
  const byPath = new Map(pages.map((page) => [page.path, page]))
  const sections = applyOrder(pages, byPath)

  const index = () => ({
    items: pages.map((page) => ({
      path: page.path,
      title: page.title,
      section: page.section,
      description: page.description,
      headings: page.headings,
    })),
    sections,
  })

  const page = (name) => {
    const found = byPath.get(name)
    if (!found) return null
    const position = pages.indexOf(found)
    const prev = position > 0 ? pages[position - 1] : null
    const next = position < pages.length - 1 ? pages[position + 1] : null
    return {
      path: found.path,
      title: found.title,
      section: found.section,
      headings: found.headings,
      markdown: found.markdown,
      words: found.words,
      prev: prev ? { path: prev.path, title: prev.title } : null,
      next: next ? { path: next.path, title: next.title } : null,
    }
  }

  const search = (query, limit = 20) => {
    const needle = (query ?? '').trim().toLowerCase()
    if (!needle) return []
    const tokens = needle.split(/\s+/).filter(Boolean)
    const hits = []
    for (const candidate of pages) {
      const title = candidate.title.toLowerCase()
      const headings = candidate.headings.map((heading) => heading.text).join(' \n ').toLowerCase()
      const body = candidate.plain.toLowerCase()
      let score = 0
      if (title.includes(needle)) score += title === needle ? 180 : 120
      if (headings.includes(needle)) score += 45
      if (body.includes(needle)) score += 12
      let covered = true
      for (const token of tokens) {
        let hit = false
        if (title.includes(token)) {
          score += 30
          hit = true
        }
        if (headings.includes(token)) {
          score += 10
          hit = true
        }
        if (body.includes(token)) {
          score += 3
          hit = true
        }
        if (candidate.path.toLowerCase().includes(token)) {
          score += 5
          hit = true
        }
        if (!hit) {
          covered = false
          break
        }
      }
      if (!covered || score === 0) continue
      hits.push({
        path: candidate.path,
        title: candidate.title,
        section: candidate.section,
        snippet: snippetFor(candidate, needle, tokens),
        score,
      })
    }
    hits.sort((left, right) => right.score - left.score)
    return hits.slice(0, limit)
  }

  const asset = (name) => {
    const clean = posix.normalize(name)
    if (!clean.startsWith('images/') || clean.includes('..')) return null
    const full = join(root, clean)
    try {
      if (!statSync(full).isFile()) return null
    } catch {
      return null
    }
    return { body: readFileSync(full), type: contentType(clean) }
  }

  return { index, page, search, asset, count: pages.length }
}

function contentType(name) {
  switch (extname(name).toLowerCase()) {
    case '.png':
      return 'image/png'
    case '.jpg':
    case '.jpeg':
      return 'image/jpeg'
    case '.gif':
      return 'image/gif'
    case '.svg':
      return 'image/svg+xml'
    case '.webp':
      return 'image/webp'
    case '.avif':
      return 'image/avif'
    case '.md':
      return 'text/markdown; charset=utf-8'
    default:
      return 'application/octet-stream'
  }
}
