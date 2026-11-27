import { useId, useState, type ReactNode } from 'react'
import { Button } from '@astryxdesign/core/Button'
import { IconButton } from '@astryxdesign/core/IconButton'
import { Icon } from '@astryxdesign/core/Icon'
import { Stack } from '@astryxdesign/core/Stack'
import { Text } from '@astryxdesign/core/Text'
import { Popover } from '@astryxdesign/core/Popover'
import { Tab, TabList } from '@astryxdesign/core/TabList'
import { SegmentedControl, SegmentedControlItem } from '@astryxdesign/core/SegmentedControl'
import { SwatchIcon } from '@heroicons/react/24/outline'
import { tr } from '../../shared/i18n/messages'
import { useLocale } from '../../shared/i18n/locale'
import { interfacePaletteIds, interfacePalettes, type ColorMode, type InterfacePalette } from '../../shared/theme/palettes'
import TerminalPalettePicker from './TerminalPalettePicker'
import type { Preferences } from './preferences'

type Appearance = Pick<Preferences,'mode'|'interfacePalette'|'terminalPalette'>
type Gallery = 'interface' | 'terminal'

const interfaceName = (id: InterfacePalette) => tr(interfacePalettes[id].label)
const previewHeight = 'var(--spacing-8)'
const optionBorder = 'var(--border-width) solid var(--color-border)'

function PreviewOption({ name, selected, onSelect, children }: {
  name:string; selected:boolean; onSelect:()=>void; children:ReactNode
}) {
  return <Button label={name} tooltip={name} aria-pressed={selected}
    variant="ghost" size="sm" onClick={onSelect}
    style={{minWidth:0,width:'100%',height:'auto',padding:'var(--spacing-1)',flexDirection:'column',alignItems:'stretch',
      justifyContent:'flex-start',
      backgroundColor:selected?'var(--color-neutral)':undefined,
      boxShadow:selected?'inset 0 0 0 var(--border-width) var(--color-accent)':undefined}}>
    <Stack as="span" width="100%" gap={0.5} style={{minWidth:0}}>
      {children}
      <Stack as="span" direction="horizontal" gap={1} justify="center" align="center" style={{minHeight:'var(--spacing-6)',minWidth:0}}>
        <Text type="supporting" maxLines={2}
          style={{textAlign:'center',whiteSpace:'normal',overflowWrap:'anywhere',color:'inherit'}}>{name}</Text>
      </Stack>
    </Stack>
  </Button>
}

function InterfaceSample({ id }: { id:InterfacePalette }) {
  const tone = (role:string) => `var(--color-preview-${id}-${role})`
  return <Stack as="span" aria-hidden="true" direction="horizontal" width="100%" height={previewHeight} gap={1} padding={1}
    style={{background:tone('body'),border:optionBorder,borderRadius:'var(--radius-inner)',overflow:'hidden'}}>
    <Stack as="span" gap={1} width="24%" padding={1} style={{background:tone('surface'),borderRadius:'var(--radius-inner)',flexShrink:0}}>
      <Stack as="span" height="var(--spacing-1)" style={{background:tone('accent'),borderRadius:'var(--radius-full)'}}/>
      <Stack as="span" height="var(--spacing-0-5)" style={{background:tone('text'),opacity:.35,borderRadius:'var(--radius-full)'}}/>
    </Stack>
    <Stack as="span" padding={1} gap={1} style={{flex:1,minWidth:0,background:tone('surface'),borderRadius:'var(--radius-inner)'}}>
      <Stack as="span" height="var(--spacing-0-5)" width="46%" style={{background:tone('text'),opacity:.65,borderRadius:'var(--radius-full)'}}/>
      <Stack as="span" height="var(--spacing-0-5)" width="78%" style={{background:tone('text'),opacity:.25,borderRadius:'var(--radius-full)'}}/>
      <Stack as="span" height="var(--spacing-1)" width="30%" style={{background:tone('accent'),borderRadius:'var(--radius-full)'}}/>
    </Stack>
  </Stack>
}

export default function WorkspaceAppearancePicker({ value, systemMode, onChange, onOpenChange }: {
  value: Appearance; systemMode: ColorMode; onChange: (patch: Partial<Appearance>) => void
  onOpenChange?: (open:boolean) => void
}) {
  useLocale()
  const [gallery,setGallery]=useState<Gallery>('interface')
  const id=useId()
  const label=tr("配色选择")
  return <Popover label={label} isModal={false} closeButtonLabel={tr("关闭")} placement="above" onOpenChange={onOpenChange}
    width="calc(var(--spacing-10) * 8.5)" padding={0}
    content={<Stack gap={1} padding={2} style={{maxHeight:'min(80dvh, calc(var(--spacing-10) * 10))',overflowY:'auto'}}>
      <Stack direction="horizontal" align="center" justify="between" gap={1} wrap="wrap">
        <TabList role="tablist" aria-label={label} value={gallery} onChange={next=>{if(next==='interface'||next==='terminal')setGallery(next)}} size="sm">
          <Tab id={id+'-interface'} value="interface" label={tr("界面配色")} panelId={id+'-interface-panel'} onFocus={()=>setGallery('interface')}/>
          <Tab id={id+'-terminal'} value="terminal" label={tr("终端配色")} panelId={id+'-terminal-panel'} onFocus={()=>setGallery('terminal')}/>
        </TabList>
        {gallery==='interface'&&<Text type="supporting" color="secondary" maxLines={1}>{tr("当前")} · {interfaceName(value.interfacePalette)}</Text>}
      </Stack>
      <Stack role="tabpanel" id={id+'-interface-panel'} aria-labelledby={id+'-interface'} hidden={gallery!=='interface'}
        gap={2} style={{display:gallery==='interface'?'flex':'none'}}>
        <SegmentedControl data-palette-mode-bar="" label={tr("外观模式")} value={value.mode} size="sm" layout="fill"
          onChange={mode=>{if(mode==='system'||mode==='light'||mode==='dark')onChange({mode})}}>
          <SegmentedControlItem value="system" label={tr("跟随系统")}/>
          <SegmentedControlItem value="light" label={tr("亮色")}/>
          <SegmentedControlItem value="dark" label={tr("暗色")}/>
        </SegmentedControl>
        <Stack gap={1} style={{display:'grid',gridTemplateColumns:'repeat(2,minmax(0,1fr))'}}>
          {interfacePaletteIds.map(preset=><PreviewOption key={preset} name={interfaceName(preset)} selected={value.interfacePalette===preset}
            onSelect={()=>onChange({interfacePalette:preset})}><InterfaceSample id={preset}/></PreviewOption>)}
        </Stack>
      </Stack>
      <Stack role="tabpanel" id={id+'-terminal-panel'} aria-labelledby={id+'-terminal'} hidden={gallery!=='terminal'}
        style={{display:gallery==='terminal'?'flex':'none'}}>
        <TerminalPalettePicker value={value.terminalPalette} systemMode={systemMode} onChange={terminalPalette=>onChange({terminalPalette})}/>
      </Stack>
    </Stack>}>
    <IconButton id="workspace-appearance" label={label} tooltip={label} size="sm" variant="ghost" icon={<Icon icon={SwatchIcon} size="sm"/>}/>
  </Popover>
}
