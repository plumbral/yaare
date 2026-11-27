import type { SVGProps } from 'react'

export function WrapTextIcon(props: SVGProps<SVGSVGElement>) {
  return <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5} {...props}>
    <path strokeLinecap="round" strokeLinejoin="round" d="M3.75 6.75h16.5M3.75 12h12.75a3 3 0 1 1 0 6h-3.75m0 0 2.25-2.25M12.75 18 15 20.25M3.75 18h5.25" />
  </svg>
}
