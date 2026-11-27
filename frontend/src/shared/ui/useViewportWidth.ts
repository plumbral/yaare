import { useEffect, useState } from 'react'

export function useViewportWidth() {
  const [width, setWidth] = useState(() => window.innerWidth)
  useEffect(() => {
    let frame = 0
    const resize = () => {
      if (frame) return
      frame = requestAnimationFrame(() => { frame = 0; setWidth(window.innerWidth) })
    }
    window.addEventListener('resize', resize)
    return () => { window.removeEventListener('resize', resize); cancelAnimationFrame(frame) }
  }, [])
  return width
}
