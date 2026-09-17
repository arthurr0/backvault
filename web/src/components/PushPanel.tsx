import { CodeBlock } from '@/components/ui/Copyable'

export function PushPanel({ slug, expectedIntervalMinutes }: { slug: string; expectedIntervalMinutes?: number }) {
  const origin = typeof window === 'undefined' ? 'https://backvault.example.com' : window.location.origin
  const ingestUrl = `${origin}/api/v1/ingest/${slug || 'job-slug'}`
  const curl = [
    `curl --fail --show-error \\`,
    `  -H "Authorization: Bearer $BACKVAULT_TOKEN" \\`,
    `  -H "X-Backvault-Filename: dump.sql.gz" \\`,
    `  -H "X-Backvault-Sha256: $(sha256sum dump.sql.gz | cut -d' ' -f1)" \\`,
    `  --data-binary @dump.sql.gz \\`,
    `  "${ingestUrl}?packed=1"`,
  ].join('\n')
  const cli = [
    `export BACKVAULT_URL=${origin}`,
    `export BACKVAULT_TOKEN=bvt_replace_with_a_token`,
    `backvault push --job ${slug || 'job-slug'} --file dump.sql.gz --packed`,
  ].join('\n')
  const script = [
    `BACKVAULT_URL=${origin} \\`,
    `BACKVAULT_TOKEN=bvt_replace_with_a_token \\`,
    `BACKVAULT_JOB=${slug || 'job-slug'} \\`,
    `  ./scripts/backup-postgres.sh`,
  ].join('\n')

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted">
        This job receives data through the ingest API. Create an API token with the{' '}
        <span className="font-mono text-text">ingest</span> scope in Settings, restrict it to this job
        slug, then push from the host that holds the data.
      </p>
      <div className="space-y-1.5">
        <p className="text-[13px] font-medium text-text">Ingest endpoint</p>
        <CodeBlock code={ingestUrl} />
      </div>
      <div className="space-y-1.5">
        <p className="text-[13px] font-medium text-text">With curl</p>
        <CodeBlock code={curl} />
      </div>
      <div className="space-y-1.5">
        <p className="text-[13px] font-medium text-text">With the Backvault CLI</p>
        <CodeBlock code={cli} />
      </div>
      <div className="space-y-1.5">
        <p className="text-[13px] font-medium text-text">With a standalone script</p>
        <CodeBlock code={script} />
      </div>
      {expectedIntervalMinutes ? (
        <p className="text-xs text-muted">
          Backvault marks this job overdue when no successful push arrives within{' '}
          {expectedIntervalMinutes} minutes.
        </p>
      ) : (
        <p className="text-xs text-muted">
          Set an expected interval in the job editor so Backvault can warn you when a push stops arriving.
        </p>
      )}
    </div>
  )
}
