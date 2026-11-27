import { useEffect, useState } from 'react'

export type ListSort<Key extends string> = { key: Key; direction: 'ascending' | 'descending' } | null

export function readListSort<Key extends string>(value: unknown, keys: readonly Key[]): ListSort<Key> {
  if (!value || typeof value !== 'object' || !('key' in value) || !('direction' in value)) return null
  const key = keys.find(key => key === value.key)
  return key && (value.direction === 'ascending' || value.direction === 'descending')
    ? { key, direction: value.direction } : null
}

export function nextListSort<Key extends string>(current: ListSort<Key>, key: Key): ListSort<Key> {
  if (current?.key !== key) return { key, direction: 'ascending' }
  return current.direction === 'ascending' ? { key, direction: 'descending' } : null
}

export function compareListValues(a: string | number | undefined, b: typeof a, collator: Intl.Collator, direction: 'ascending' | 'descending') {
  if (a === undefined) return b === undefined ? 0 : 1
  if (b === undefined) return -1
  const order = typeof a === 'number' && typeof b === 'number' ? a - b : collator.compare(String(a), String(b))
  return direction === 'ascending' ? order : -order
}

export function useLocalListView<T>(storageKey: string, normalize: (value: unknown) => T) {
  const [value, setValue] = useState(() => {
    try { return normalize(JSON.parse(localStorage.getItem(storageKey) ?? 'null')) }
    catch { return normalize(null) }
  })
  useEffect(() => {
    try { localStorage.setItem(storageKey, JSON.stringify(value)) }
    catch {}
  }, [storageKey, value])
  return [value, setValue] as const
}
