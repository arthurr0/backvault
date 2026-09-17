import { useState } from 'react'
import { useNavigate } from 'react-router'
import { Plus } from 'lucide-react'
import { errorMessage } from '@/api/client'
import { useJob, useRestoreArtifact, useSources } from '@/api/hooks'
import { Button } from '@/components/ui/Button'
import { Dialog } from '@/components/ui/Dialog'
import { FieldShell } from '@/components/ui/Field'
import { Input, Select, Switch } from '@/components/ui/Input'
import { SecretInput } from '@/components/form/SecretInput'
import {
  ParamsEditor,
  newParamRow,
  paramRowsToConfig,
  type ParamRow,
} from '@/components/form/ParamsEditor'
import { useToast } from '@/components/ui/Toast'
import { cn } from '@/lib/utils'
import type { Artifact, RestoreMode } from '@/api/types'

const PRESET_PARAMS: Record<string, string[]> = {
  postgres: ['database', 'clean', 'create', 'if_exists', 'no_owner', 'single_transaction'],
  mysql: ['database'],
  mongodb: ['database', 'drop', 'rename_from', 'rename_to'],
  docker: ['volume'],
  command: ['restore_command'],
  ssh: ['restore_command'],
}

export function RestoreDialog({
  artifact,
  onClose,
}: {
  artifact: Artifact | null
  onClose: () => void
}) {
  const [mode, setMode] = useState<RestoreMode>('path')
  const [targetPath, setTargetPath] = useState('')
  const [extract, setExtract] = useState(false)
  const [targetSourceId, setTargetSourceId] = useState('')
  const [passphrase, setPassphrase] = useState('')
  const [paramRows, setParamRows] = useState<ParamRow[]>([])
  const restore = useRestoreArtifact()
  const sources = useSources()
  const job = useJob(artifact?.jobSlug)
  const toast = useToast()
  const navigate = useNavigate()

  if (!artifact) return null

  const sameKindSources = (sources.data ?? []).filter((source) => source.kind === artifact.sourceKind)
  const isTar = artifact.extension === 'tar'
  const presets = PRESET_PARAMS[artifact.sourceKind] ?? []
  const params = paramRowsToConfig(paramRows)
  const hasParams = Object.keys(params).length > 0

  const submit = () => {
    restore.mutate(
      {
        id: artifact.id,
        mode,
        targetPath: mode === 'path' ? targetPath : undefined,
        extract: mode === 'path' ? extract : undefined,
        targetSourceId: mode === 'source' ? targetSourceId || undefined : undefined,
        passphrase: passphrase || undefined,
        params: mode === 'source' && hasParams ? params : undefined,
      },
      {
        onSuccess: (result) => {
          toast.success('Restore queued', artifact.filename)
          onClose()
          navigate(`/runs/${result.run.id}`)
        },
        onError: (error) => toast.error('The restore could not be started', errorMessage(error)),
      },
    )
  }

  const disabled =
    (mode === 'path' && !targetPath.trim()) ||
    (mode === 'source' && sameKindSources.length === 0)

  return (
    <Dialog
      open
      onClose={onClose}
      width="lg"
      title="Restore artifact"
      description={artifact.filename}
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" disabled={disabled} loading={restore.isPending} onClick={submit}>
            Start restore
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <div className="grid gap-2 sm:grid-cols-2">
          {(
            [
              {
                id: 'path' as RestoreMode,
                title: 'Restore to a path',
                text: 'Write the unpacked artifact to a path on the Backvault host.',
              },
              {
                id: 'source' as RestoreMode,
                title: 'Restore into the source',
                text: 'Pipe the artifact back into the source driver, for example psql or mysql.',
              },
            ]
          ).map((option) => (
            <button
              key={option.id}
              type="button"
              onClick={() => setMode(option.id)}
              className={cn(
                'rounded-xl border p-3 text-left transition-colors',
                mode === option.id
                  ? 'border-accent bg-accent-soft'
                  : 'border-border bg-surface hover:border-border-strong',
              )}
            >
              <span className="block text-[13px] font-medium text-text">{option.title}</span>
              <span className="mt-0.5 block text-xs text-muted">{option.text}</span>
            </button>
          ))}
        </div>

        {mode === 'path' ? (
          <div className="space-y-4">
            <FieldShell
              label="Target path"
              htmlFor="restore-path"
              required
              help="An absolute path on the host running Backvault."
            >
              <Input
                id="restore-path"
                className="font-mono"
                placeholder="/var/restore/database.sql"
                value={targetPath}
                onChange={(event) => setTargetPath(event.target.value)}
              />
            </FieldShell>
            {isTar ? (
              <FieldShell
                label="Extract the archive"
                htmlFor="restore-extract"
                help="Unpack the tar into the target directory instead of writing a single file."
              >
                <div className="flex h-9 items-center gap-2.5">
                  <Switch
                    id="restore-extract"
                    label="Extract the archive"
                    checked={extract}
                    onChange={setExtract}
                  />
                  <span className="text-sm text-muted">{extract ? 'Extract' : 'Write one file'}</span>
                </div>
              </FieldShell>
            ) : null}
          </div>
        ) : (
          <div className="space-y-4">
            <FieldShell
              label="Target source"
              htmlFor="restore-source"
              help={
                sameKindSources.length
                  ? `Must be a ${artifact.sourceKind} source. Leave on the job source to restore in place.`
                  : `No ${artifact.sourceKind} source is configured, so this mode is unavailable.`
              }
            >
              <Select
                id="restore-source"
                value={targetSourceId}
                onChange={(event) => setTargetSourceId(event.target.value)}
              >
                <option value="">
                  {job.data?.sourceName ? `Job source (${job.data.sourceName})` : 'Job source'}
                </option>
                {sameKindSources.map((source) => (
                  <option key={source.id} value={source.id}>
                    {source.name}
                  </option>
                ))}
              </Select>
            </FieldShell>

            <FieldShell
              label="Driver parameters"
              help={`Passed to the ${artifact.sourceKind} restorer. Booleans accept true or false. Empty rows are ignored.`}
              hint={hasParams ? `${Object.keys(params).length} set` : undefined}
            >
              <div className="space-y-2">
                {presets.length ? (
                  <div className="flex flex-wrap gap-1.5">
                    {presets.map((key) => {
                      const used = paramRows.some((row) => row.key.trim() === key)
                      return (
                        <button
                          key={key}
                          type="button"
                          disabled={used}
                          onClick={() => setParamRows([...paramRows, newParamRow(key)])}
                          className={cn(
                            'inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 font-mono text-[12px] transition-colors',
                            used
                              ? 'cursor-not-allowed border-border bg-surface-3 text-soft'
                              : 'border-border bg-surface text-muted hover:border-accent hover:text-text',
                          )}
                        >
                          <Plus className="size-3" aria-hidden="true" />
                          {key}
                        </button>
                      )
                    })}
                  </div>
                ) : null}
                <ParamsEditor
                  rows={paramRows}
                  onChange={setParamRows}
                  suggestions={presets}
                  emptyHint="No parameters, the driver uses its own defaults."
                />
              </div>
            </FieldShell>
          </div>
        )}

        {artifact.encryption === 'age' ? (
          <FieldShell
            label="Passphrase override"
            htmlFor="restore-passphrase"
            help="Leave empty to use the passphrase stored on the job."
          >
            <SecretInput
              id="restore-passphrase"
              value={passphrase}
              onChange={setPassphrase}
              placeholder="Optional"
            />
          </FieldShell>
        ) : null}

        <div className="rounded-lg border border-border bg-surface-2 p-3 text-xs text-muted">
          <p>
            Restore runs appear in the run list with kind <span className="font-mono">restore</span> and
            stream their log live. The artifact itself is never modified.
          </p>
        </div>
      </div>
    </Dialog>
  )
}
