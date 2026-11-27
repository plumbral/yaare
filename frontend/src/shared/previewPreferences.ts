import { isMyGo } from 'mygo-runtime'

export const previewPreferencesKey = 'yaare.preview.preferences.v1'

export function usesPreviewStorage() {
  return !isMyGo() && import.meta.env?.MODE !== 'browser'
}
