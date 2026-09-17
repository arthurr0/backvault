import { NavLink } from 'react-router'
import {
  Activity,
  Archive,
  Bell,
  Database,
  HardDrive,
  LayoutDashboard,
  ListChecks,
  Settings,
} from 'lucide-react'
import { Logo } from '@/components/Logo'
import { useVersion } from '@/api/hooks'
import { cn } from '@/lib/utils'

const NAV = [
  { to: '/', label: 'Dashboard', icon: LayoutDashboard, end: true },
  { to: '/jobs', label: 'Jobs', icon: ListChecks, end: false },
  { to: '/runs', label: 'Runs', icon: Activity, end: false },
  { to: '/artifacts', label: 'Artifacts', icon: Archive, end: false },
  { to: '/sources', label: 'Sources', icon: Database, end: false },
  { to: '/destinations', label: 'Destinations', icon: HardDrive, end: false },
  { to: '/notifications', label: 'Notifications', icon: Bell, end: false },
  { to: '/settings', label: 'Settings', icon: Settings, end: false },
]

function displayVersion(version: string): string {
  const trimmed = version.trim()
  if (!trimmed) return ''
  return /^\d/.test(trimmed) ? `v${trimmed}` : trimmed
}

export function SidebarContent({ onNavigate }: { onNavigate?: () => void }) {
  const version = useVersion()
  return (
    <div className="flex h-full flex-col">
      <div className="flex h-16 items-center px-4">
        <NavLink to="/" onClick={onNavigate} className="rounded-lg">
          <Logo />
        </NavLink>
      </div>
      <nav aria-label="Main" className="flex-1 space-y-0.5 px-3 py-2">
        {NAV.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.end}
            onClick={onNavigate}
            className={({ isActive }) =>
              cn(
                'flex items-center gap-3 rounded-lg px-3 py-2 text-[13.5px] font-medium transition-colors',
                isActive
                  ? 'bg-accent-soft text-text'
                  : 'text-muted hover:bg-surface-2 hover:text-text',
              )
            }
          >
            {({ isActive }) => (
              <>
                <item.icon
                  aria-hidden="true"
                  className={cn('size-4', isActive ? 'text-accent' : 'text-soft')}
                />
                {item.label}
              </>
            )}
          </NavLink>
        ))}
      </nav>
      <div className="border-t border-border px-4 py-3">
        <p className="text-[11px] text-soft">Every backup, accounted for.</p>
        <p className="mt-0.5 font-mono text-[11px] text-soft">
          {version.data ? displayVersion(version.data.version) : ''}
        </p>
      </div>
    </div>
  )
}
