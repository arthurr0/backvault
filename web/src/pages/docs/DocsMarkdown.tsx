import {
  Children,
  createContext,
  isValidElement,
  useContext,
  useRef,
  useState,
  type ComponentProps,
  type ReactNode,
} from 'react'
import { Link, useLocation } from 'react-router'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeSlug from 'rehype-slug'
import rehypeHighlight from 'rehype-highlight'
import bash from 'highlight.js/lib/languages/bash'
import dockerfile from 'highlight.js/lib/languages/dockerfile'
import go from 'highlight.js/lib/languages/go'
import ini from 'highlight.js/lib/languages/ini'
import json from 'highlight.js/lib/languages/json'
import nginx from 'highlight.js/lib/languages/nginx'
import plaintext from 'highlight.js/lib/languages/plaintext'
import sql from 'highlight.js/lib/languages/sql'
import typescript from 'highlight.js/lib/languages/typescript'
import yaml from 'highlight.js/lib/languages/yaml'
import { Check, Copy, ExternalLink } from 'lucide-react'
import { resolveAssetUrl, resolveDocLink, routeForDoc } from './paths'

const languages = { bash, dockerfile, go, ini, json, nginx, plaintext, sql, typescript, yaml }

const aliases = {
  bash: ['sh', 'shell', 'console'],
  ini: ['toml'],
  plaintext: ['text', 'txt', 'log'],
  typescript: ['ts'],
  yaml: ['yml'],
}

type MarkdownProps = ComponentProps<typeof ReactMarkdown>

const remarkPlugins: MarkdownProps['remarkPlugins'] = [remarkGfm]
const rehypePlugins: MarkdownProps['rehypePlugins'] = [
  rehypeSlug,
  [rehypeHighlight, { detect: false, languages, aliases }],
]

const DocPathContext = createContext('')

function fallbackCopy(text: string): boolean {
  const area = document.createElement('textarea')
  area.value = text
  area.setAttribute('readonly', '')
  area.style.position = 'fixed'
  area.style.opacity = '0'
  document.body.appendChild(area)
  area.select()
  let ok: boolean
  try {
    ok = document.execCommand('copy')
  } catch {
    ok = false
  }
  document.body.removeChild(area)
  return ok
}

function languageOf(children: ReactNode): string {
  const first = Children.toArray(children)[0]
  if (!isValidElement<{ className?: string }>(first)) return 'code'
  const match = /language-([\w-]+)/.exec(first.props.className ?? '')
  return match ? match[1] : 'code'
}

function CodeBlock({ children, ...rest }: ComponentProps<'pre'>) {
  const ref = useRef<HTMLPreElement>(null)
  const [copied, setCopied] = useState(false)
  const label = languageOf(children)

  const flash = () => {
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1600)
  }

  const copy = () => {
    const text = ref.current?.textContent ?? ''
    if (!text) return
    if (navigator.clipboard?.writeText) {
      navigator.clipboard.writeText(text).then(flash, () => {
        if (fallbackCopy(text)) flash()
      })
      return
    }
    if (fallbackCopy(text)) flash()
  }

  return (
    <div className="docs-code">
      <div className="docs-code-bar">
        <span className="docs-code-lang">{label}</span>
        <button
          type="button"
          className="docs-code-copy"
          onClick={copy}
          aria-label={copied ? 'Snippet copied' : `Copy the ${label} snippet`}
        >
          {copied ? <Check className="size-3.5" aria-hidden="true" /> : <Copy className="size-3.5" aria-hidden="true" />}
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      <pre ref={ref} {...rest}>
        {children}
      </pre>
    </div>
  )
}

function Anchor({ href, children, ...rest }: ComponentProps<'a'>) {
  const from = useContext(DocPathContext)
  const location = useLocation()

  if (!href) return <a {...rest}>{children}</a>
  if (href.startsWith('#')) {
    return (
      <Link className="docs-link" to={{ pathname: location.pathname, hash: href }}>
        {children}
      </Link>
    )
  }
  if (/^https?:\/\//i.test(href)) {
    return (
      <a className="docs-link" href={href} target="_blank" rel="noopener noreferrer">
        {children}
        <ExternalLink className="docs-link-icon" aria-hidden="true" />
      </a>
    )
  }
  if (/^[a-z][a-z0-9+.-]*:/i.test(href)) {
    return (
      <a className="docs-link" href={href} {...rest}>
        {children}
      </a>
    )
  }
  const resolved = resolveDocLink(from, href)
  if (resolved) {
    return (
      <Link className="docs-link" to={`${routeForDoc(resolved.path)}${resolved.hash}`}>
        {children}
      </Link>
    )
  }
  return (
    <a className="docs-link" href={href} {...rest}>
      {children}
    </a>
  )
}

function Image({ src, alt, ...rest }: ComponentProps<'img'>) {
  const from = useContext(DocPathContext)
  const resolved = typeof src === 'string' ? resolveAssetUrl(from, src) : src
  return <img className="docs-image" loading="lazy" src={resolved} alt={alt ?? ''} {...rest} />
}

function Table({ children, ...rest }: ComponentProps<'table'>) {
  return (
    <div className="docs-table">
      <table {...rest}>{children}</table>
    </div>
  )
}

const components: Components = { a: Anchor, img: Image, pre: CodeBlock, table: Table }

export function DocsMarkdown({ docPath, markdown }: { docPath: string; markdown: string }) {
  return (
    <DocPathContext.Provider value={docPath}>
      <ReactMarkdown remarkPlugins={remarkPlugins} rehypePlugins={rehypePlugins} components={components}>
        {markdown}
      </ReactMarkdown>
    </DocPathContext.Provider>
  )
}
