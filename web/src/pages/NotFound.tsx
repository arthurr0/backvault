import { Link } from 'react-router'
import { usePageMeta } from '@/lib/pageMeta'

export function NotFoundPage() {
  usePageMeta('Not found')
  return (
    <div className="flex min-h-[50vh] flex-col items-center justify-center gap-3 text-center">
      <p className="font-mono text-4xl text-accent">404</p>
      <h1 className="text-lg font-semibold text-text">This page does not exist</h1>
      <p className="max-w-md text-sm text-muted">
        The address may have changed, or the object was removed.
      </p>
      <Link to="/" className="rounded-lg px-3 py-2 text-sm font-medium text-accent hover:underline">
        Back to the dashboard
      </Link>
    </div>
  )
}
