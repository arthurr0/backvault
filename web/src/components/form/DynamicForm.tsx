import { useMemo, useState } from 'react'
import { ChevronRight } from 'lucide-react'
import { SECRET_MASK, type Config, type ConfigValue, type Field } from '@/api/types'
import { FieldShell, useFieldId } from '@/components/ui/Field'
import { Input, Select, Switch, Textarea } from '@/components/ui/Input'
import { cn } from '@/lib/utils'
import { SecretInput } from './SecretInput'
import { TagInput } from './TagInput'

export function isSecretField(field: Field): boolean {
  return field.secret || field.type === 'secret'
}

export function showIfMatches(field: Field, config: Config): boolean {
  if (!field.showIf) return true
  for (const [key, expected] of Object.entries(field.showIf)) {
    const actual = config[key]
    if (typeof expected === 'boolean') {
      const current = actual === true || actual === 'true' || actual === 1
      if (current !== expected) return false
      continue
    }
    if (Array.isArray(expected)) {
      if (!expected.map(String).includes(String(actual ?? ''))) return false
      continue
    }
    if (String(expected ?? '') !== String(actual ?? '')) return false
  }
  return true
}

export function defaultsFor(fields: Field[]): Config {
  const config: Config = {}
  for (const field of fields) {
    if (field.default === undefined || field.default === null) {
      if (field.type === 'bool') config[field.name] = false
      else if (field.type === 'list') config[field.name] = []
      else config[field.name] = ''
      continue
    }
    config[field.name] = field.default
  }
  return config
}

export function normalizeConfig(fields: Field[], config: Config): Config {
  const out: Config = {}
  for (const field of fields) {
    if (!showIfMatches(field, config)) continue
    const raw = config[field.name]
    switch (field.type) {
      case 'bool':
        out[field.name] = raw === true || raw === 'true'
        break
      case 'int':
      case 'port': {
        if (raw === '' || raw === undefined || raw === null) break
        const num = Number(raw)
        out[field.name] = Number.isFinite(num) ? num : 0
        break
      }
      case 'list':
        out[field.name] = Array.isArray(raw) ? raw : raw ? String(raw).split(/[\n,]+/).map((s) => s.trim()).filter(Boolean) : []
        break
      default:
        if (raw === undefined || raw === null || raw === '') break
        out[field.name] = String(raw)
    }
  }
  return out
}

interface GroupedFields {
  name: string
  advanced: boolean
  fields: Field[]
}

function groupFields(fields: Field[]): GroupedFields[] {
  const groups: GroupedFields[] = []
  const index = new Map<string, GroupedFields>()
  for (const field of fields) {
    const advanced = Boolean(field.advanced)
    const name = field.group?.trim() || (advanced ? 'Advanced' : 'General')
    const key = `${name}::${advanced && name === 'Advanced'}`
    let group = index.get(key)
    if (!group) {
      group = { name, advanced: advanced || name.toLowerCase() === 'advanced', fields: [] }
      index.set(key, group)
      groups.push(group)
    }
    group.advanced = group.advanced && (advanced || name.toLowerCase() === 'advanced')
    group.fields.push(field)
  }
  return groups
}

export interface DynamicFormProps {
  fields: Field[]
  value: Config
  onChange: (value: Config) => void
  errors?: Record<string, string>
  storedSecrets?: boolean
  disabled?: boolean
  className?: string
}

export function DynamicForm({
  fields,
  value,
  onChange,
  errors,
  storedSecrets,
  disabled,
  className,
}: DynamicFormProps) {
  const groups = useMemo(() => groupFields(fields), [fields])
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>(() => {
    const initial: Record<string, boolean> = {}
    for (const group of groupFields(fields)) initial[group.name] = group.advanced
    return initial
  })

  const setValue = (name: string, next: ConfigValue) => {
    onChange({ ...value, [name]: next })
  }

  if (!fields.length) {
    return <p className="text-sm text-muted">This driver has no options to configure.</p>
  }

  return (
    <div className={cn('flex flex-col gap-5', className)}>
      {groups.map((group) => {
        const visible = group.fields.filter((field) => showIfMatches(field, value))
        if (!visible.length) return null
        const isCollapsible = groups.length > 1
        const isOpen = !isCollapsible || !collapsed[group.name]
        return (
          <fieldset key={group.name} className="min-w-0">
            {isCollapsible ? (
              <button
                type="button"
                aria-expanded={isOpen}
                onClick={() => setCollapsed((prev) => ({ ...prev, [group.name]: !prev[group.name] }))}
                className="mb-3 flex w-full items-center gap-1.5 border-b border-border pb-2 text-left"
              >
                <ChevronRight
                  aria-hidden="true"
                  className={cn('size-4 text-soft transition-transform', isOpen && 'rotate-90')}
                />
                <span className="text-[13px] font-semibold text-text">{group.name}</span>
                <span className="text-xs text-soft">
                  {visible.length} {visible.length === 1 ? 'option' : 'options'}
                </span>
              </button>
            ) : null}
            {isOpen ? (
              <div className="grid gap-4 sm:grid-cols-2">
                {visible.map((field) => (
                  <DynamicField
                    key={field.name}
                    field={field}
                    value={value[field.name]}
                    error={errors?.[field.name]}
                    disabled={disabled}
                    storedSecrets={storedSecrets}
                    onChange={(next) => setValue(field.name, next)}
                  />
                ))}
              </div>
            ) : null}
          </fieldset>
        )
      })}
    </div>
  )
}

function DynamicField({
  field,
  value,
  error,
  disabled,
  storedSecrets,
  onChange,
}: {
  field: Field
  value: ConfigValue
  error?: string
  disabled?: boolean
  storedSecrets?: boolean
  onChange: (value: ConfigValue) => void
}) {
  const id = useFieldId(`field-${field.name}`)
  const wide = field.type === 'text' || field.type === 'list' || field.type === 'path'

  const control = () => {
    switch (field.type) {
      case 'bool':
        return (
          <div className="flex h-9 items-center gap-2.5">
            <Switch
              id={id}
              label={field.label}
              disabled={disabled}
              checked={value === true || value === 'true'}
              onChange={(next) => onChange(next)}
            />
            <span className="text-sm text-muted">
              {value === true || value === 'true' ? 'Enabled' : 'Disabled'}
            </span>
          </div>
        )
      case 'select':
        return (
          <Select
            id={id}
            disabled={disabled}
            value={String(value ?? '')}
            onChange={(event) => onChange(event.target.value)}
          >
            {!field.required ? <option value="">Not set</option> : null}
            {(field.options ?? []).map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </Select>
        )
      case 'secret':
        return (
          <SecretInput
            id={id}
            storedMask={storedSecrets}
            value={String(value ?? '')}
            placeholder={field.placeholder}
            onChange={(next) => onChange(next)}
          />
        )
      case 'text':
        return (
          <Textarea
            id={id}
            disabled={disabled}
            rows={4}
            placeholder={field.placeholder}
            value={String(value ?? '')}
            onChange={(event) => onChange(event.target.value)}
          />
        )
      case 'list':
        return (
          <TagInput
            id={id}
            disabled={disabled}
            invalid={Boolean(error)}
            placeholder={field.placeholder ?? 'Type a value and press Enter, or paste a list'}
            value={Array.isArray(value) ? value : []}
            onChange={(next) => onChange(next)}
          />
        )
      case 'int':
      case 'port':
        return (
          <Input
            id={id}
            type="number"
            inputMode="numeric"
            disabled={disabled}
            min={field.type === 'port' ? 1 : undefined}
            max={field.type === 'port' ? 65535 : undefined}
            placeholder={field.placeholder}
            value={value === undefined || value === null ? '' : String(value)}
            onChange={(event) => onChange(event.target.value === '' ? '' : Number(event.target.value))}
            className="font-mono"
          />
        )
      default:
        return (
          <Input
            id={id}
            disabled={disabled}
            spellCheck={false}
            placeholder={field.placeholder}
            value={String(value ?? '')}
            onChange={(event) => onChange(event.target.value)}
            className={field.type === 'path' ? 'font-mono' : undefined}
          />
        )
    }
  }

  return (
    <FieldShell
      label={field.label}
      htmlFor={id}
      required={field.required}
      help={field.help}
      error={error}
      className={wide ? 'sm:col-span-2' : undefined}
    >
      {control()}
    </FieldShell>
  )
}

export function maskStoredSecrets(fields: Field[], config: Config): Config {
  const out = { ...config }
  for (const field of fields) {
    if (isSecretField(field) && out[field.name] === undefined) out[field.name] = SECRET_MASK
  }
  return out
}
