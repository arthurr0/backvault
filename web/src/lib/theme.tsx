import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'

export type ThemePreference = 'light' | 'dark' | 'system'
export type ResolvedTheme = 'light' | 'dark'

const STORAGE_KEY = 'backvault.theme'

interface ThemeContextValue {
  preference: ThemePreference
  resolved: ResolvedTheme
  setPreference: (value: ThemePreference) => void
  toggle: () => void
}

const ThemeContext = createContext<ThemeContextValue | null>(null)

function readPreference(): ThemePreference {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored === 'light' || stored === 'dark' || stored === 'system') return stored
  } catch {
    return 'system'
  }
  return 'system'
}

function systemTheme(): ResolvedTheme {
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [preference, setPreferenceState] = useState<ThemePreference>(readPreference)
  const [systemValue, setSystemValue] = useState<ResolvedTheme>(systemTheme)

  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const handler = () => setSystemValue(media.matches ? 'dark' : 'light')
    media.addEventListener('change', handler)
    return () => media.removeEventListener('change', handler)
  }, [])

  const resolved: ResolvedTheme = preference === 'system' ? systemValue : preference

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', resolved)
  }, [resolved])

  const setPreference = useCallback((value: ThemePreference) => {
    setPreferenceState(value)
    try {
      localStorage.setItem(STORAGE_KEY, value)
    } catch {
      void 0
    }
  }, [])

  const toggle = useCallback(() => {
    setPreference(resolved === 'dark' ? 'light' : 'dark')
  }, [resolved, setPreference])

  const value = useMemo(
    () => ({ preference, resolved, setPreference, toggle }),
    [preference, resolved, setPreference, toggle],
  )

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext)
  if (!ctx) throw new Error('useTheme must be used inside ThemeProvider')
  return ctx
}
