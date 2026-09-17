export function cn(...values: Array<string | false | null | undefined>): string {
  return values.filter(Boolean).join(' ')
}

export async function copyText(value: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(value)
      return true
    }
  } catch {
    void 0
  }
  try {
    const area = document.createElement('textarea')
    area.value = value
    area.setAttribute('readonly', '')
    area.style.position = 'fixed'
    area.style.opacity = '0'
    document.body.appendChild(area)
    area.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(area)
    return ok
  } catch {
    return false
  }
}

export function passphraseStrength(value: string): { score: number; label: string; hint: string } {
  if (!value) return { score: 0, label: 'Empty', hint: 'A passphrase is required for age encryption' }
  let score = 0
  if (value.length >= 10) score += 1
  if (value.length >= 16) score += 1
  if (value.length >= 24) score += 1
  if (/[a-z]/.test(value) && /[A-Z]/.test(value)) score += 1
  if (/[0-9]/.test(value)) score += 1
  if (/[^A-Za-z0-9]/.test(value)) score += 1
  const capped = Math.min(4, Math.round((score / 6) * 4))
  const labels = ['Very weak', 'Weak', 'Fair', 'Strong', 'Very strong']
  const hints = [
    'Use at least 16 characters',
    'Add length, this is the only thing protecting the archive',
    'Longer is better than complex, aim for 20 characters or more',
    'Store this passphrase somewhere safe, lost passphrases mean lost backups',
    'Store this passphrase somewhere safe, lost passphrases mean lost backups',
  ]
  return { score: capped, label: labels[capped], hint: hints[capped] }
}

export function isTerminalStatus(status: string): boolean {
  return status === 'success' || status === 'warning' || status === 'failed' || status === 'canceled'
}

export function uid(prefix = 'id'): string {
  return `${prefix}-${Math.random().toString(36).slice(2, 10)}`
}
