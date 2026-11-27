const sustainedOutputMs = 250
const quietOutputMs = 1800

interface OutputWindow {
  started: number
  lastOutput: number
  timer?: ReturnType<typeof setTimeout>
}

export function createSessionOutputActivity(onChange: (id: string, active: boolean) => void) {
  const sessions = new Map<string, OutputWindow>()

  return {
    record(id: string) {
      const now = performance.now()
      const session = sessions.get(id)
      if (!session || (session.timer === undefined && now - session.lastOutput >= quietOutputMs)) {
        sessions.set(id, { started: now, lastOutput: now })
        return
      }
      session.lastOutput = now
      if (session.timer !== undefined || now - session.started < sustainedOutputMs) return

      const expire = () => {
        const remaining = quietOutputMs - (performance.now() - session.lastOutput)
        if (remaining > 0) {
          session.timer = setTimeout(expire, remaining)
          return
        }
        sessions.delete(id)
        onChange(id, false)
      }
      session.timer = setTimeout(expire, quietOutputMs)
      onChange(id, true)
    },
    clear(id: string) {
      const session = sessions.get(id)
      sessions.delete(id)
      if (session?.timer === undefined) return
      clearTimeout(session.timer)
      onChange(id, false)
    },
    dispose() {
      for (const session of sessions.values()) clearTimeout(session.timer)
      sessions.clear()
    },
  }
}
