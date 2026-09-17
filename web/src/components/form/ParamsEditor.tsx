import { useId } from 'react'
import { Plus, X } from 'lucide-react'
import { Button, IconButton } from '@/components/ui/Button'
import { Input } from '@/components/ui/Input'
import { uid } from '@/lib/utils'

export interface ParamRow {
  id: string
  key: string
  value: string
}

export function newParamRow(key = '', value = ''): ParamRow {
  return { id: uid('param'), key, value }
}

export function paramRowsToConfig(rows: ParamRow[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const row of rows) {
    const key = row.key.trim()
    if (key) out[key] = row.value.trim()
  }
  return out
}

export interface ParamsEditorProps {
  rows: ParamRow[]
  onChange: (rows: ParamRow[]) => void
  suggestions?: string[]
  keyPlaceholder?: string
  valuePlaceholder?: string
  addLabel?: string
  emptyHint?: string
}

export function ParamsEditor({
  rows,
  onChange,
  suggestions = [],
  keyPlaceholder = 'name',
  valuePlaceholder = 'value',
  addLabel = 'Add parameter',
  emptyHint,
}: ParamsEditorProps) {
  const listId = useId()

  const patch = (id: string, changes: Partial<ParamRow>) =>
    onChange(rows.map((row) => (row.id === id ? { ...row, ...changes } : row)))

  return (
    <div className="space-y-2">
      {suggestions.length ? (
        <datalist id={listId}>
          {suggestions.map((suggestion) => (
            <option key={suggestion} value={suggestion} />
          ))}
        </datalist>
      ) : null}

      {rows.length === 0 && emptyHint ? <p className="text-xs text-soft">{emptyHint}</p> : null}

      {rows.map((row, index) => (
        <div key={row.id} className="flex items-center gap-2">
          <Input
            aria-label={`Parameter ${index + 1} name`}
            className="w-44 font-mono text-[12.5px]"
            list={suggestions.length ? listId : undefined}
            placeholder={keyPlaceholder}
            value={row.key}
            onChange={(event) => patch(row.id, { key: event.target.value })}
          />
          <Input
            aria-label={`Parameter ${index + 1} value`}
            className="flex-1 font-mono text-[12.5px]"
            placeholder={valuePlaceholder}
            value={row.value}
            onChange={(event) => patch(row.id, { value: event.target.value })}
          />
          <IconButton
            label={`Remove parameter ${index + 1}`}
            size="sm"
            onClick={() => onChange(rows.filter((item) => item.id !== row.id))}
          >
            <X className="size-4" />
          </IconButton>
        </div>
      ))}

      <Button size="sm" icon={<Plus className="size-4" />} onClick={() => onChange([...rows, newParamRow()])}>
        {addLabel}
      </Button>
    </div>
  )
}
