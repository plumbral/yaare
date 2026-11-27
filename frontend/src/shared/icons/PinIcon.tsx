import type { SVGProps } from 'react'

export function PinOutlineIcon(props: SVGProps<SVGSVGElement>) {
  return <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5} {...props}>
    <path strokeLinecap="round" strokeLinejoin="round" d="M8 3h8l-1 7 4 4v2H5v-2l4-4-1-7ZM12 16v6"/>
  </svg>
}
export function PinFilledIcon(props: SVGProps<SVGSVGElement>) {
  return <svg viewBox="0 0 24 24" fill="currentColor" {...props}>
    <path d="M7.26 3.11A.75.75 0 0 1 8 2.25h8a.75.75 0 0 1 .74.86l-.95 6.62 3.74 3.74c.14.14.22.33.22.53v2a.75.75 0 0 1-.75.75h-6.25V22a.75.75 0 0 1-1.5 0v-5.25H5a.75.75 0 0 1-.75-.75v-2c0-.2.08-.39.22-.53l3.74-3.74-.95-6.62Z"/>
  </svg>
}
