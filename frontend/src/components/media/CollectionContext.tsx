import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { deleteCollection, fetchCollection, saveCollection } from '../../lib/media'
import type { CollectionData, CollectionEdit } from '../../lib/media'
import { errorText } from '../../lib/format'

interface CollectionState extends CollectionData {
  loading: boolean; error: string | null; reload: () => Promise<void>
  save: (id: number, edit: CollectionEdit) => Promise<void>; remove: (id: number) => Promise<void>
}
const unavailable = async () => { throw new Error('请先登录账号') }
const CollectionContext = createContext<CollectionState>({ loggedIn: false, entries: [], loading: false, error: null, reload: async () => {}, save: unavailable, remove: unavailable })
export const useCollection = () => useContext(CollectionContext)

export function CollectionProvider({ children }: { children: ReactNode }) {
  const [data, setData] = useState<CollectionData>({ loggedIn: false, entries: [] })
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const version = useRef(0)
  const reload = useCallback(async () => {
    const request = ++version.current
    try { const next = await fetchCollection(); if (request === version.current) { setData(next); setError(null) } }
    catch (err) { if (request === version.current) setError(errorText(err, '读取收藏失败')) }
    finally { if (request === version.current) setLoading(false) }
  }, [])
  useEffect(() => {
    void reload()
    const update = () => { void reload() }
    const accountChanged = () => { setData({ loggedIn: false, entries: [] }); setLoading(true); void reload() }
    window.addEventListener('nagare:account-changed', accountChanged)
    window.addEventListener('focus', update)
    return () => { version.current++; window.removeEventListener('nagare:account-changed', accountChanged); window.removeEventListener('focus', update) }
  }, [reload])
  const save = async (id: number, edit: CollectionEdit) => { try { await saveCollection(id, edit) } finally { await reload() } }
  const remove = async (id: number) => { try { await deleteCollection(id) } finally { await reload() } }
  return <CollectionContext.Provider value={{ ...data, loading, error, reload, save, remove }}>{children}</CollectionContext.Provider>
}
