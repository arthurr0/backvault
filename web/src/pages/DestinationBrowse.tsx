import { useState } from 'react'
import { Link, useParams } from 'react-router'
import { ArrowLeft, ChevronRight, File, Folder, TriangleAlert } from 'lucide-react'
import { errorMessage } from '@/api/client'
import { useDestination, useDestinationBrowse } from '@/api/hooks'
import { Button } from '@/components/ui/Button'
import { Card } from '@/components/ui/Card'
import { EmptyState } from '@/components/ui/EmptyState'
import { SkeletonRows } from '@/components/ui/Skeleton'
import { TableWrap, Td, Th, Tr } from '@/components/ui/Table'
import { RelativeTime } from '@/components/ui/Time'
import { formatBytes, formatNumber } from '@/lib/format'
import { usePageMeta } from '@/lib/pageMeta'

export function DestinationBrowsePage() {
  const { id = '' } = useParams()
  const destination = useDestination(id)
  const [prefix, setPrefix] = useState('')
  const objects = useDestinationBrowse(id, prefix)

  usePageMeta(destination.data ? `Browse ${destination.data.name}` : 'Browse destination', [
    { label: 'Destinations', to: '/destinations' },
    { label: destination.data?.name ?? id },
  ])

  const segments = prefix.split('/').filter(Boolean)

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <Link to="/destinations">
            <Button size="sm" icon={<ArrowLeft className="size-4" />}>
              Back
            </Button>
          </Link>
          <h2 className="text-lg font-semibold text-text">{destination.data?.name ?? 'Destination'}</h2>
        </div>
        {destination.data ? (
          <p className="text-xs text-muted">
            {formatBytes(destination.data.usedBytes)} ·{' '}
            {formatNumber(destination.data.artifactCount)} artifacts · {destination.data.kind}
          </p>
        ) : null}
      </div>

      <nav aria-label="Path" className="flex flex-wrap items-center gap-1 text-[13px] text-muted">
        <button type="button" className="rounded px-1 hover:text-accent" onClick={() => setPrefix('')}>
          root
        </button>
        {segments.map((segment, index) => (
          <span key={`${segment}-${index}`} className="flex items-center gap-1">
            <ChevronRight className="size-3" aria-hidden="true" />
            <button
              type="button"
              className="rounded px-1 font-mono hover:text-accent"
              onClick={() => setPrefix(`${segments.slice(0, index + 1).join('/')}/`)}
            >
              {segment}
            </button>
          </span>
        ))}
      </nav>

      <Card bodyClassName="p-0">
        {objects.isLoading ? (
          <div className="p-4">
            <SkeletonRows rows={6} />
          </div>
        ) : objects.isError ? (
          <div className="p-4">
            <EmptyState
              icon={<TriangleAlert className="size-6" />}
              title="This destination could not be listed"
              description={errorMessage(objects.error)}
            />
          </div>
        ) : (objects.data ?? []).length === 0 ? (
          <div className="p-4">
            <EmptyState
              icon={<Folder className="size-6" />}
              title="Nothing here"
              description="This prefix holds no objects yet."
            />
          </div>
        ) : (
          <TableWrap>
            <thead>
              <tr>
                <Th>Path</Th>
                <Th align="right">Size</Th>
                <Th align="right">Modified</Th>
              </tr>
            </thead>
            <tbody>
              {(objects.data ?? []).map((object) => (
                <Tr
                  key={object.path}
                  onClick={object.isDir ? () => setPrefix(`${object.path.replace(/\/$/, '')}/`) : undefined}
                >
                  <Td>
                    <span className="flex items-center gap-2">
                      {object.isDir ? (
                        <Folder className="size-4 text-accent" aria-hidden="true" />
                      ) : (
                        <File className="size-4 text-soft" aria-hidden="true" />
                      )}
                      <span className="truncate font-mono text-[12.5px]">{object.path}</span>
                    </span>
                  </Td>
                  <Td align="right" className="font-mono text-muted">
                    {object.isDir ? '-' : formatBytes(object.size)}
                  </Td>
                  <Td align="right" className="text-muted">
                    <RelativeTime value={object.modTime} />
                  </Td>
                </Tr>
              ))}
            </tbody>
          </TableWrap>
        )}
      </Card>
    </div>
  )
}
