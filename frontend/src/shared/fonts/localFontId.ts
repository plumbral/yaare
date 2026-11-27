export type LocalFont = `local:${string}`
export function localFontName(value: unknown): string | null {
  if (typeof value !== 'string' || !value.startsWith('local:')) return null
  const name = value.slice(6)
  return name && name === name.trim() && Array.from(name).length <= 128 && !/[\u0000-\u001f\u007f-\u009f]/u.test(name) ? name : null
}
export const isLocalFont = (value: unknown): value is LocalFont => localFontName(value) !== null
export const localFontId = (name: string): LocalFont => `local:${name}`
