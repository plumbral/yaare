export type FontPlatform = 'windows' | 'macos' | 'linux' | 'other'

export function detectFontPlatform(value?: string): FontPlatform {
  const platform = value ?? (typeof navigator === 'undefined' ? '' :
    (navigator as Navigator & {userAgentData?: {platform?: string}}).userAgentData?.platform || navigator.platform)
  if (/mac|darwin/i.test(platform)) return 'macos'
  if (/^win/i.test(platform)) return 'windows'
  if (/linux|x11|cros/i.test(platform)) return 'linux'
  return 'other'
}
export function systemMonoFamilies(platform = detectFontPlatform()): string[] {
  return platform === 'windows' ? ['Consolas', 'Courier New'] : platform === 'macos' ? ['Menlo', 'Monaco'] : platform === 'linux' ? ['DejaVu Sans Mono', 'Liberation Mono', 'Noto Sans Mono'] : []
}
export function systemMonoName(platform = detectFontPlatform()): string | null {
  return platform === 'windows' ? 'Consolas' : platform === 'macos' ? 'Menlo' : null
}
