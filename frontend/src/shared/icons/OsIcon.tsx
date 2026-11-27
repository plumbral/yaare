import { tr } from '../i18n/messages'
import { useLocale, getLanguage } from '../i18n/locale'
import { createContext, useContext, type SVGProps } from 'react'
import { readCustomIcon, type CustomHostIcon } from './customIcon'
import { Icon } from '@astryxdesign/core/Icon'
import paths from './os/paths.json'
import vendors from './cloudVendors.json'
import { builtInIconColors } from './builtInIconColors'

export type OsIconName = keyof typeof paths | keyof typeof vendors | CustomHostIcon
export type SystemIconName = keyof typeof paths
export type BuiltInIconStyle = 'monochrome' | 'color'
export const BuiltInIconStyleContext = createContext<BuiltInIconStyle>('monochrome')
export const isSystemIcon = (name: string): name is SystemIconName => Object.hasOwn(paths, name)
const systemIconOptions = [
  { value: 'linux', get label() { return tr("Linux（默认）") } }, { value: 'ubuntu', label: 'Ubuntu' },
  { value: 'debian', label: 'Debian' }, { value: 'alpinelinux', label: 'Alpine Linux' },
  { value: 'archlinux', label: 'Arch Linux' }, { value: 'fedora', label: 'Fedora' },
  { value: 'redhat', label: 'Red Hat Enterprise Linux' }, { value: 'centos', label: 'CentOS / CentOS Stream' },
  { value: 'rockylinux', label: 'Rocky Linux' }, { value: 'almalinux', label: 'AlmaLinux' },
  { value: 'opensuse', label: 'openSUSE' }, { value: 'gentoo', label: 'Gentoo' },
  { value: 'nixos', label: 'NixOS' }, { value: 'slackware', label: 'Slackware' },
  { value: 'devuan', label: 'Devuan' }, { value: 'raspberrypi', label: 'Raspberry Pi OS' },
  { value: 'manjaro', label: 'Manjaro' }, { value: 'linuxmint', label: 'Linux Mint' },
  { value: 'kalilinux', label: 'Kali Linux' }, { value: 'freebsd', label: 'FreeBSD' },
  { value: 'openbsd', label: 'OpenBSD' },
] as const

type IconOption = { value: OsIconName; label: string; aliases?: readonly string[] }
const vendorOptions = (Object.keys(vendors) as (keyof typeof vendors)[]).map(value => ({
  value, label: vendors[value].label, aliases: vendors[value].aliases,
}))
export const osIconOptions: readonly IconOption[] = [...systemIconOptions, ...vendorOptions]

const iconNameSort = new Intl.Collator('en', { sensitivity: 'base' })
const searchKey = (text: string) => text.normalize('NFKC').toLowerCase().replace(/[^\p{L}\p{N}]+/gu, '')

export type HostIconCategory = 'system' | 'cloud' | 'custom'

export function searchHostIcons(query: string, category?: HostIconCategory, customValues: readonly (OsIconName | undefined)[] = []): IconOption[] {
  const term = searchKey(query)
  const shortCode = /^[a-z0-9]{1,2}$/.test(term)
  const options: readonly IconOption[] = category === 'custom'
    ? [...new Set(customValues)].flatMap(value => {
        const icon = readCustomIcon(value)
        return icon ? [icon] : []
      })
    : category === 'system' ? systemIconOptions : category === 'cloud' ? vendorOptions : osIconOptions
  return options
    .filter(({ label, value, aliases }) => !term ||
      searchKey(label).includes(term) || (category !== 'custom' && searchKey(value.replace(/^cloud-/, '')).includes(term)) ||
      aliases?.some(alias => shortCode ? searchKey(alias) === term : searchKey(alias).includes(term)))
    .sort((a, b) => iconNameSort.compare(a.label, b.label))
}

const vendorGlyphs = Object.fromEntries(Object.entries(vendors).map(([id, { asset }]) => {
  const url = new URL(`./cloud/${asset}`, import.meta.url).href
  const mask = `url("${url}") center / contain no-repeat`
  function VendorGlyph(props: SVGProps<SVGSVGElement>) {
    return <svg {...props} viewBox="0 0 24 24" aria-hidden="true" focusable="false"
      style={{ ...props.style, mask }}>
      <rect width="24" height="24" fill="currentColor" />
    </svg>
  }
  return [id, VendorGlyph]
}))

type Glyph = {viewBox:string;paths:{d:string;transform?:string;fillRule?:'evenodd'}[]}
const glyphs = Object.fromEntries(Object.entries(paths).map(([name, value]) => [name,
  function OsGlyph(props: SVGProps<SVGSVGElement>) {
  useLocale()

    const glyph=value as Glyph
    return <svg {...props} viewBox={glyph.viewBox} focusable="false">{glyph.paths.map((path,index)=><path key={index} fill="currentColor" d={path.d} transform={path.transform} fillRule={path.fillRule}/>)}</svg>
  },
]))

export default function OsIcon({ name = 'linux', size = 'sm' }: { name?: OsIconName; size?: 'sm' | 'md' | 'lg' }) {
  useLocale()
  const style = useContext(BuiltInIconStyleContext)

  const custom = readCustomIcon(name)
  if (custom) {
    const edge = { sm: 'var(--spacing-4)', md: 'var(--spacing-5)', lg: 'var(--spacing-6)' }[size]
    return <img alt="" src={custom.src} draggable={false} style={{ width: edge, height: edge, objectFit: 'contain', flexShrink: 0 }} />
  }
  const colors = builtInIconColors[name as keyof typeof builtInIconColors] ?? builtInIconColors.linux
  return <Icon icon={glyphs[name] ?? vendorGlyphs[name] ?? glyphs.linux!} size={size}
    style={style === 'color' ? { color: `light-dark(${colors[0]}, ${colors[1]})` } : undefined} />
}
