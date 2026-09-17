import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type Dispatch,
  type ReactNode,
  type SetStateAction,
} from 'react'

export interface Crumb {
  label: string
  to?: string
}

export interface PageMeta {
  title: string
  crumbs: Crumb[]
}

const MetaContext = createContext<PageMeta>({ title: '', crumbs: [] })
const SetMetaContext = createContext<Dispatch<SetStateAction<PageMeta>> | null>(null)

export function PageMetaProvider({ children }: { children: ReactNode }) {
  const [meta, setMeta] = useState<PageMeta>({ title: '', crumbs: [] })
  const value = useMemo(() => meta, [meta])
  return (
    <SetMetaContext.Provider value={setMeta}>
      <MetaContext.Provider value={value}>{children}</MetaContext.Provider>
    </SetMetaContext.Provider>
  )
}

export function usePageMetaValue(): PageMeta {
  return useContext(MetaContext)
}

export function usePageMeta(title: string, crumbs: Crumb[] = []) {
  const setMeta = useContext(SetMetaContext)
  const serialized = JSON.stringify(crumbs)
  useEffect(() => {
    if (!setMeta) return
    setMeta({ title, crumbs: JSON.parse(serialized) as Crumb[] })
    document.title = title ? `${title} · Backvault` : 'Backvault'
  }, [setMeta, title, serialized])
}
