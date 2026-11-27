export function needsDraftReview(baseRevision: number | undefined, currentRevision: number | undefined, existing: boolean, found: boolean) {
  if (baseRevision !== undefined) return baseRevision !== currentRevision
  return !existing && found
}

export function reviewDraft<T extends { id: string }>(original: T, draft: T, latest: T): T {
  const reviewed = { ...draft }
  const keys = Object.keys({ ...original, ...latest }) as (keyof T)[]
  for (const key of keys) {
    if (JSON.stringify(original[key]) !== JSON.stringify(latest[key])) {
      reviewed[key] = latest[key]
    }
  }
  return reviewed
}
