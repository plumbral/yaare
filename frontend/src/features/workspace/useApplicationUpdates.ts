import { useEffect, useSyncExternalStore } from 'react'
import { ApplicationService } from '../../shared/generated/mygo'
import { desktopAvailable } from '../../shared/bridge/application'
import { createUpdateController, firstUpdateCheckDelay, type UpdateStatus } from './applicationUpdates'

const fallback = (): UpdateStatus => ({ state: 'preview', currentVersion: '', latestVersion: '', notes: '', checkedAt: 0 })
const controller = createUpdateController({
  status: async () => desktopAvailable() && import.meta.env.MODE!=='browser' ? ApplicationService.getUpdateStatus() : fallback(),
  check: async automatic => desktopAvailable() && import.meta.env.MODE!=='browser' ? ApplicationService.checkForUpdates(automatic) : fallback(),
}, () => { try { return localStorage } catch { return undefined } })

export function useApplicationUpdates() {
  return {
    ...useSyncExternalStore(controller.subscribe, controller.getSnapshot),
    check: () => controller.check(false),
    setAutomatic: controller.setAutomatic,
  }
}
export function useApplicationUpdateRunner() {
  const state = useApplicationUpdates()
  useEffect(() => {
    if (import.meta.env.MODE === 'browser') return
    void controller.initialize()
    if (!desktopAvailable()) return
    let active = true
    async function poll(startup = false) {
      const nextCheck = await controller.automaticCheck(startup)
      if (active) timer = setTimeout(poll, nextCheck)
    }
    let timer = setTimeout(() => void poll(true), firstUpdateCheckDelay)
    return () => { active = false; clearTimeout(timer) }
  }, [])
  return state.status.state === 'available'
}
