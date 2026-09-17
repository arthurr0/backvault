import { createElement } from 'react'
import {
  Activity,
  Archive,
  Bell,
  Box,
  Boxes,
  CloudUpload,
  Cloud,
  Container,
  Database,
  FileArchive,
  FileText,
  Folder,
  Globe,
  HardDrive,
  Inbox,
  Key,
  Layers,
  Leaf,
  Mail,
  MessageCircle,
  MessageSquare,
  Network,
  Radio,
  Send,
  Server,
  SquareTerminal,
  Terminal,
  Upload,
  Webhook,
  Zap,
  type LucideIcon,
} from 'lucide-react'

const ICONS: Record<string, LucideIcon> = {
  activity: Activity,
  archive: Archive,
  bell: Bell,
  box: Box,
  boxes: Boxes,
  cloud: Cloud,
  'cloud-upload': CloudUpload,
  command: SquareTerminal,
  container: Container,
  database: Database,
  disc: HardDrive,
  discord: MessageCircle,
  docker: Container,
  email: Mail,
  'file-archive': FileArchive,
  'file-text': FileText,
  files: Folder,
  folder: Folder,
  globe: Globe,
  'hard-drive': HardDrive,
  inbox: Inbox,
  key: Key,
  layers: Layers,
  leaf: Leaf,
  local: HardDrive,
  mail: Mail,
  'message-circle': MessageCircle,
  'message-square': MessageSquare,
  mongodb: Leaf,
  mysql: Database,
  network: Network,
  ntfy: Radio,
  postgres: Database,
  postgresql: Database,
  push: Upload,
  radio: Radio,
  redis: Zap,
  s3: Cloud,
  send: Send,
  server: Server,
  sftp: Server,
  slack: MessageSquare,
  sqlite: FileArchive,
  ssh: Terminal,
  telegram: Send,
  terminal: Terminal,
  'square-terminal': SquareTerminal,
  upload: Upload,
  webdav: Globe,
  webhook: Webhook,
  zap: Zap,
}

export function DriverIcon({
  icon,
  kind,
  className,
}: {
  icon?: string
  kind?: string
  className?: string
}) {
  return createElement(driverIcon(icon, kind), { className, 'aria-hidden': true })
}

export function driverIcon(name: string | undefined, fallbackKind?: string): LucideIcon {
  const key = (name ?? '').trim().toLowerCase()
  if (key && ICONS[key]) return ICONS[key]
  const kind = (fallbackKind ?? '').trim().toLowerCase()
  if (kind && ICONS[kind]) return ICONS[kind]
  return Box
}
