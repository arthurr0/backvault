import { useMemo } from 'react'
import { useMe } from '@/api/hooks'
import type { Scope } from '@/api/types'

export interface Capabilities {
  admin: boolean
  run: boolean
  read: boolean
}

const NONE: Capabilities = { admin: false, run: false, read: false }

export function capabilitiesOf(scopes: readonly string[] | undefined): Capabilities {
  if (!scopes) return NONE
  const has = (scope: Scope) => scopes.includes(scope)
  const admin = has('admin')
  const run = admin || has('run')
  return { admin, run, read: run || has('read') }
}

export function useCan(): Capabilities {
  const me = useMe()
  const scopes = me.isSuccess ? me.data.scopes : undefined
  return useMemo(() => capabilitiesOf(scopes), [scopes])
}
