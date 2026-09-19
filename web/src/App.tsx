import { lazy } from 'react'
import { Navigate, Route, Routes, useLocation } from 'react-router'
import { LoaderCircle } from 'lucide-react'
import { useMe, useSetupStatus } from '@/api/hooks'
import { AppLayout } from '@/components/layout/AppLayout'
import { ArtifactsPage } from '@/pages/Artifacts'
import { DashboardPage } from '@/pages/Dashboard'
import { DestinationBrowsePage } from '@/pages/DestinationBrowse'
import { DestinationsPage } from '@/pages/Destinations'
import { JobDetailPage } from '@/pages/JobDetail'
import { JobEditorPage } from '@/pages/JobEditor'
import { JobsPage } from '@/pages/Jobs'
import { LoginPage } from '@/pages/Login'
import { NotFoundPage } from '@/pages/NotFound'
import { NotificationsPage } from '@/pages/Notifications'
import { RunDetailPage } from '@/pages/RunDetail'
import { RunsPage } from '@/pages/Runs'
import { SettingsPage } from '@/pages/Settings'
import { SetupPage } from '@/pages/Setup'
import { SourcesPage } from '@/pages/Sources'

const DocsPage = lazy(() => import('@/pages/docs/DocsPage'))

function FullScreenLoader() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-bg">
      <LoaderCircle className="size-6 animate-spin text-accent" aria-label="Loading" />
    </div>
  )
}

export function App() {
  const location = useLocation()
  const setup = useSetupStatus()
  const onAuthRoute = location.pathname === '/login' || location.pathname === '/setup'
  const me = useMe({ enabled: setup.data?.needsSetup === false && !onAuthRoute })

  if (setup.isLoading) return <FullScreenLoader />

  const needsSetup = setup.data?.needsSetup === true
  const isAuthRoute = onAuthRoute

  if (needsSetup && location.pathname !== '/setup') return <Navigate to="/setup" replace />
  if (!needsSetup && location.pathname === '/setup') return <Navigate to="/" replace />

  if (!isAuthRoute) {
    if (me.isLoading) return <FullScreenLoader />
    if (me.isError || !me.data) {
      const next = location.pathname !== '/' ? `?next=${encodeURIComponent(location.pathname)}` : ''
      return <Navigate to={`/login${next}`} replace />
    }
  }

  return (
    <Routes>
      <Route path="/setup" element={<SetupPage />} />
      <Route path="/login" element={<LoginPage />} />
      <Route element={<AppLayout me={me.data} />}>
        <Route index element={<DashboardPage />} />
        <Route path="jobs" element={<JobsPage />} />
        <Route path="jobs/new" element={<JobEditorPage />} />
        <Route path="jobs/:slug" element={<JobDetailPage />} />
        <Route path="jobs/:slug/edit" element={<JobEditorPage />} />
        <Route path="runs" element={<RunsPage />} />
        <Route path="runs/:id" element={<RunDetailPage />} />
        <Route path="artifacts" element={<ArtifactsPage />} />
        <Route path="sources" element={<SourcesPage />} />
        <Route path="destinations" element={<DestinationsPage />} />
        <Route path="destinations/:id/browse" element={<DestinationBrowsePage />} />
        <Route path="notifications" element={<NotificationsPage />} />
        <Route path="docs/*" element={<DocsPage />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  )
}
