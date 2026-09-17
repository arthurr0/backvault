import { useMemo, useState } from 'react'
import { CircleCheck, CircleX, Plug } from 'lucide-react'
import { errorMessage, fieldErrors } from '@/api/client'
import {
  useSaveDestination,
  useSaveSource,
  useTestDestination,
  useTestSource,
} from '@/api/hooks'
import type { Config, Destination, DriverSpec, Source } from '@/api/types'
import { DriverPicker } from '@/components/form/DriverPicker'
import { DynamicForm, defaultsFor, maskStoredSecrets, normalizeConfig } from '@/components/form/DynamicForm'
import { TagInput } from '@/components/form/TagInput'
import { Button } from '@/components/ui/Button'
import { Dialog } from '@/components/ui/Dialog'
import { FieldShell } from '@/components/ui/Field'
import { Input, Textarea } from '@/components/ui/Input'
import { useToast } from '@/components/ui/Toast'
import { formatDuration } from '@/lib/format'

export type ResourceKind = 'source' | 'destination'

export function DriverResourceDialog({
  kind,
  specs,
  record,
  open,
  onClose,
}: {
  kind: ResourceKind
  specs: DriverSpec[]
  record: Source | Destination | null
  open: boolean
  onClose: () => void
}) {
  if (!open) return null
  return (
    <ResourceForm
      key={record?.id ?? 'new'}
      kind={kind}
      specs={specs}
      record={record}
      onClose={onClose}
    />
  )
}

function initialConfig(specs: DriverSpec[], record: Source | Destination | null): Config {
  if (!record) return {}
  const driverSpec = specs.find((item) => item.kind === record.kind)
  if (!driverSpec) return { ...record.config }
  return maskStoredSecrets(driverSpec.fields, { ...defaultsFor(driverSpec.fields), ...record.config })
}

function ResourceForm({
  kind,
  specs,
  record,
  onClose,
}: {
  kind: ResourceKind
  specs: DriverSpec[]
  record: Source | Destination | null
  onClose: () => void
}) {
  const isEdit = Boolean(record?.id)
  const toast = useToast()
  const saveSource = useSaveSource(kind === 'source' ? record?.id : undefined)
  const saveDestination = useSaveDestination(kind === 'destination' ? record?.id : undefined)
  const testSource = useTestSource()
  const testDestination = useTestDestination()
  const test = kind === 'source' ? testSource : testDestination
  const saveError = kind === 'source' ? saveSource.error : saveDestination.error
  const savePending = kind === 'source' ? saveSource.isPending : saveDestination.isPending

  const [driverKind, setDriverKind] = useState(record?.kind ?? '')
  const [name, setName] = useState(record?.name ?? '')
  const [description, setDescription] = useState(record?.description ?? '')
  const [tags, setTags] = useState<string[]>(record?.tags ?? [])
  const [config, setConfig] = useState<Config>(() => initialConfig(specs, record))

  const spec = useMemo(() => specs.find((item) => item.kind === driverKind), [specs, driverKind])

  const pickDriver = (next: string) => {
    setDriverKind(next)
    const driverSpec = specs.find((item) => item.kind === next)
    setConfig(driverSpec ? defaultsFor(driverSpec.fields) : {})
    if (!name && driverSpec) setName(driverSpec.label)
    test.reset()
  }

  const errors = fieldErrors(saveError)

  const submit = () => {
    if (!spec) return
    const input = {
      name: name.trim(),
      kind: spec.kind,
      description: description.trim(),
      config: normalizeConfig(spec.fields, config),
      tags,
    }
    const handlers = {
      onSuccess: (saved: { name: string }) => {
        toast.success(isEdit ? 'Saved' : 'Created', saved.name)
        onClose()
      },
      onError: (error: unknown) => toast.error(`The ${kind} could not be saved`, errorMessage(error)),
    }
    if (kind === 'source') saveSource.mutate(input, handlers)
    else saveDestination.mutate(input, handlers)
  }

  const runTest = () => {
    if (!spec) return
    test.mutate(
      { id: record?.id, kind: spec.kind, config: normalizeConfig(spec.fields, config) },
      {
        onError: (error) => toast.error('The test could not be run', errorMessage(error)),
      },
    )
  }

  const title = isEdit
    ? `Edit ${kind}`
    : kind === 'source'
      ? 'New source'
      : 'New destination'

  return (
    <Dialog
      open
      onClose={onClose}
      width="xl"
      title={title}
      description={
        kind === 'source'
          ? 'Sources describe what to back up.'
          : 'Destinations describe where artifacts are stored.'
      }
      footer={
        <>
          {spec ? (
            <Button
              className="mr-auto"
              icon={<Plug className="size-4" />}
              loading={test.isPending}
              onClick={runTest}
            >
              Test connection
            </Button>
          ) : null}
          <Button onClick={onClose}>Cancel</Button>
          <Button
            variant="primary"
            disabled={!spec || !name.trim()}
            loading={savePending}
            onClick={submit}
          >
            {isEdit ? 'Save' : 'Create'}
          </Button>
        </>
      }
    >
      <div className="space-y-5">
        {!isEdit ? (
          <DriverPicker specs={specs} value={driverKind} onChange={pickDriver} />
        ) : null}

        {spec ? (
          <>
            <div className="grid gap-4 sm:grid-cols-2">
              <FieldShell label="Name" htmlFor="resource-name" required error={errors.name}>
                <Input
                  id="resource-name"
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  placeholder={spec.label}
                />
              </FieldShell>
              <FieldShell label="Tags" htmlFor="resource-tags">
                <TagInput id="resource-tags" value={tags} onChange={setTags} placeholder="production" />
              </FieldShell>
              <FieldShell label="Description" htmlFor="resource-description" className="sm:col-span-2">
                <Textarea
                  id="resource-description"
                  rows={2}
                  value={description}
                  onChange={(event) => setDescription(event.target.value)}
                />
              </FieldShell>
            </div>

            <div className="border-t border-border pt-4">
              <DynamicForm
                fields={spec.fields}
                value={config}
                onChange={setConfig}
                errors={errors}
                storedSecrets={isEdit}
              />
            </div>

            {spec.tools?.length ? (
              <p className="text-xs text-muted">
                This driver needs {spec.tools.join(', ')} on the Backvault host. Check availability under
                Settings, Tools.
              </p>
            ) : null}

            {test.data ? (
              <div
                className={`flex items-start gap-2 rounded-lg border p-3 text-sm ${
                  test.data.ok
                    ? 'border-success/40 bg-success/10 text-success'
                    : 'border-danger/40 bg-danger/10 text-danger'
                }`}
                role="status"
              >
                {test.data.ok ? (
                  <CircleCheck className="mt-0.5 size-4" aria-hidden="true" />
                ) : (
                  <CircleX className="mt-0.5 size-4" aria-hidden="true" />
                )}
                <div className="min-w-0">
                  <p className="font-medium">
                    {test.data.ok ? 'Connection succeeded' : 'Connection failed'}
                    {test.data.durationMs ? ` · ${formatDuration(test.data.durationMs)}` : ''}
                  </p>
                  {test.data.message ? (
                    <p className="mt-0.5 break-words text-text">{test.data.message}</p>
                  ) : null}
                </div>
              </div>
            ) : null}
          </>
        ) : (
          <p className="text-sm text-muted">Pick a driver to continue.</p>
        )}
      </div>
    </Dialog>
  )
}
