import type { Retention } from '@/api/types'

export const EMPTY_RETENTION: Retention = {
  keepLast: 0,
  keepHourly: 0,
  keepDaily: 0,
  keepWeekly: 0,
  keepMonthly: 0,
  keepYearly: 0,
  maxAgeDays: 0,
}

export function isRetentionEmpty(retention: Retention | undefined): boolean {
  if (!retention) return true
  return (
    !retention.keepLast &&
    !retention.keepHourly &&
    !retention.keepDaily &&
    !retention.keepWeekly &&
    !retention.keepMonthly &&
    !retention.keepYearly &&
    !retention.maxAgeDays
  )
}

function span(count: number, unit: string): string {
  return count === 1 ? `the last ${unit}` : `the last ${count} ${unit}s`
}

export function retentionRules(retention: Retention | undefined): string[] {
  if (!retention) return []
  const rules: string[] = []
  if (retention.keepLast) {
    rules.push(
      retention.keepLast === 1 ? 'the newest backup' : `the ${retention.keepLast} newest backups`,
    )
  }
  if (retention.keepHourly) rules.push(`one backup per hour for ${span(retention.keepHourly, 'hour')}`)
  if (retention.keepDaily) rules.push(`one backup per day for ${span(retention.keepDaily, 'day')}`)
  if (retention.keepWeekly) rules.push(`one backup per week for ${span(retention.keepWeekly, 'week')}`)
  if (retention.keepMonthly) rules.push(`one backup per month for ${span(retention.keepMonthly, 'month')}`)
  if (retention.keepYearly) rules.push(`one backup per year for ${span(retention.keepYearly, 'year')}`)
  return rules
}

export function describeRetention(retention: Retention | undefined, inherited = false): string {
  if (isRetentionEmpty(retention)) {
    return inherited
      ? 'No rules are set here, so the default retention from Settings applies.'
      : 'Nothing is deleted automatically, every artifact is kept.'
  }
  const rules = retentionRules(retention)
  const parts: string[] = []
  if (rules.length) {
    parts.push(`Backvault keeps ${joinList(rules)} on every destination.`)
  }
  if (retention?.maxAgeDays) {
    parts.push(
      `Anything older than ${retention.maxAgeDays} ${
        retention.maxAgeDays === 1 ? 'day' : 'days'
      } is removed unless one of the rules above still protects it.`,
    )
  }
  parts.push('The newest successful backup is never deleted.')
  return parts.join(' ')
}

function joinList(items: string[]): string {
  if (items.length === 1) return items[0]
  if (items.length === 2) return `${items[0]} and ${items[1]}`
  return `${items.slice(0, -1).join(', ')} and ${items[items.length - 1]}`
}

export function estimateKeptCount(retention: Retention): number {
  return (
    retention.keepLast +
    retention.keepHourly +
    retention.keepDaily +
    retention.keepWeekly +
    retention.keepMonthly +
    retention.keepYearly
  )
}
