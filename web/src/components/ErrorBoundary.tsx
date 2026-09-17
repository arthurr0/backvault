import { Component, type ErrorInfo, type ReactNode } from 'react'
import { TriangleAlert } from 'lucide-react'
import { Button } from './ui/Button'

interface Props {
  children: ReactNode
}

interface State {
  error: Error | null
}

export class ErrorBoundary extends Component<Props, State> {
  override state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  override componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('Panel error', error, info.componentStack)
  }

  override render() {
    if (!this.state.error) return this.props.children
    return (
      <div className="flex min-h-[60vh] flex-col items-center justify-center gap-4 p-8 text-center">
        <TriangleAlert className="size-8 text-danger" aria-hidden="true" />
        <div className="space-y-1">
          <h1 className="text-lg font-semibold text-text">This page could not be rendered</h1>
          <p className="max-w-lg text-sm text-muted">{this.state.error.message}</p>
        </div>
        <div className="flex gap-2">
          <Button onClick={() => this.setState({ error: null })}>Try again</Button>
          <Button variant="primary" onClick={() => window.location.assign('/')}>
            Back to dashboard
          </Button>
        </div>
      </div>
    )
  }
}
