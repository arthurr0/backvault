import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from './App'
import { ErrorBoundary } from './components/ErrorBoundary'
import { ConfirmProvider } from './components/ui/Confirm'
import { ToastProvider } from './components/ui/Toast'
import { PageMetaProvider } from './lib/pageMeta'
import { ThemeProvider } from './lib/theme'
import './index.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: (failureCount, error) => {
        const status = (error as { status?: number }).status
        if (status && status >= 400 && status < 500) return false
        return failureCount < 2
      },
      refetchOnWindowFocus: false,
      staleTime: 5_000,
    },
  },
})

const container = document.getElementById('root')
if (!container) throw new Error('Root element is missing')

createRoot(container).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <ToastProvider>
          <ConfirmProvider>
            <PageMetaProvider>
              <BrowserRouter>
                <ErrorBoundary>
                  <App />
                </ErrorBoundary>
              </BrowserRouter>
            </PageMetaProvider>
          </ConfirmProvider>
        </ToastProvider>
      </ThemeProvider>
    </QueryClientProvider>
  </StrictMode>,
)
