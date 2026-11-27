import {useState} from 'react'
import {Button} from '@astryxdesign/core/Button'
import {Stack} from '@astryxdesign/core/Stack'
import {Text} from '@astryxdesign/core/Text'
import EditorDialog from '../../shared/ui/EditorDialog'
import {tr} from '../../shared/i18n/messages'
import {presetSizes,type SplitPreset} from './terminalLayout'

export const presetLabels=()=>({side:tr('左右双屏'),stack:tr('上下双屏'),three:tr('左一右二'),four:tr('四宫格')})
const areas={side:'"a b"',stack:'"a" "b"',three:'"a b" "a c"',four:'"a b" "c d"'}
export default function SplitLayoutDialog({ids,initial,label,onApply,onClose}:{
  ids:string[];initial:SplitPreset;label:(id:string)=>string;onApply:(preset:SplitPreset,order:string[])=>void;onClose:()=>void
}){
  const [preset,setPreset]=useState(initial),[order,setOrder]=useState(ids),[picked,setPicked]=useState<number>()
  const count=presetSizes[preset],names=presetLabels()
  const slot=(i:number)=>{
    const id=order[i],name=id?label(id):tr('待连接')
    return <Button key={i} type="button" label={(i+1)+' · '+name} tooltip={name} variant={picked===i?'primary':'secondary'}
      aria-pressed={picked===i} isDisabled={!id} style={{gridArea:i<count?'abcd'[i]:undefined,minWidth:0,height:'100%'}}
      onClick={()=>{
        if(picked===undefined){setPicked(i);return}
        if(picked!==i){const next=[...order];next[i]=order[picked]!;next[picked]=id!;setOrder(next)}
        setPicked(undefined)
      }}/>
  }
  return <EditorDialog title={tr('组合为分屏')} action="edit" onClose={onClose} saveHint={tr('应用布局')}
    formProps={{onSubmit:e=>{e.preventDefault();onApply(preset,order)}}} actions={<Button label={tr('应用布局')} type="submit"/>}>
    <Stack direction="horizontal" gap={1} wrap="wrap">{(Object.keys(names) as SplitPreset[]).map(p=>
      <Button key={p} type="button" size="sm" label={names[p]} aria-pressed={preset===p} variant={preset===p?'secondary':'ghost'} onClick={()=>{setPreset(p);setPicked(undefined)}}/>)}</Stack>
    <Stack gap={1} padding={1} style={{display:'grid',gridTemplateAreas:areas[preset],gridAutoColumns:'minmax(0,1fr)',gridAutoRows:'1fr',height:'calc(var(--spacing-10) * 4)',background:'var(--color-background-body)'}}>
      {Array.from({length:count},(_,i)=>slot(i))}
    </Stack>
    {order.length>count&&<><Text type="label">{tr('保留为独立标签')}</Text><Stack direction="horizontal" gap={1} wrap="wrap">{order.slice(count).map((_,i)=>slot(i+count))}</Stack></>}
    {order.length>1&&<Text type="supporting" color="secondary">{tr('依次选择两个窗格可交换位置。')}</Text>}
  </EditorDialog>
}
