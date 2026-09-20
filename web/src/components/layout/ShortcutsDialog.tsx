import { Dialog } from '@/components/ui/Dialog'

const SHORTCUTS: Array<[string, string]> = [
  ['⌘ K / Ctrl K', 'Open the command palette'],
  ['g then j', 'Go to jobs'],
  ['g then r', 'Go to runs'],
  ['g then a', 'Go to artifacts'],
  ['g then d', 'Go to the dashboard'],
  ['g then s', 'Go to sources'],
  ['g then o', 'Go to hosts'],
  ['g then h', 'Go to the documentation'],
  ['⌘ / Ctrl /', 'Search the documentation'],
  ['?', 'Show this help'],
  ['Esc', 'Close a dialog or the palette'],
]

export function ShortcutsDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <Dialog open={open} onClose={onClose} title="Keyboard shortcuts" width="sm">
      <dl className="divide-y divide-border">
        {SHORTCUTS.map(([keys, description]) => (
          <div key={keys} className="flex items-center justify-between gap-4 py-2">
            <dt className="text-sm text-muted">{description}</dt>
            <dd>
              <kbd className="rounded border border-border bg-surface-2 px-2 py-0.5 font-mono text-[11px] text-text">
                {keys}
              </kbd>
            </dd>
          </div>
        ))}
      </dl>
    </Dialog>
  )
}
