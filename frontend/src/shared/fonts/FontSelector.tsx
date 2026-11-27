import {useEffect,useMemo,useRef,useState} from 'react'
import {Selector} from '@astryxdesign/core/Selector'
import {Stack} from '@astryxdesign/core/Stack'
import {Switch} from '@astryxdesign/core/Switch'
import {Button} from '@astryxdesign/core/Button'
import {fontAvailable,isSystemFont} from './fonts'
import {isLocalFont,localFontId,localFontName} from './localFontId'
import {localFonts,type LocalFontFamily} from './localFonts'
import {systemMonoName} from './platformFonts'
import {tr,localizeMessage} from '../i18n/messages'
import {useLocale} from '../i18n/locale'

export default function FontSelector({id,label,value,options,defaultValue,onChange,systemLabel,monospaced=false}:{id:string;label:string;value:string;options:readonly string[];defaultValue:string;onChange:(value:string)=>void;systemLabel?:string;monospaced?:boolean}){
  const language=useLocale()
  const [busy,setBusy]=useState(false)
  const [error,setError]=useState('')
  const [rows,setRows]=useState<LocalFontFamily[]>([])
  const [phase,setPhase]=useState<'loading'|'ready'|'failed'>(localFonts.available()?'loading':'ready')
  const [attempt,setAttempt]=useState(0)
  const [showAll,setShowAll]=useState(false)
  const [usable,setUsable]=useState(true)
  const currentChange=useRef(onChange);currentChange.current=onChange
  const active=useRef(true)
  useEffect(()=>{active.current=true;return()=>{active.current=false}},[])
  useEffect(()=>{
    let cancelled=false
    if(!localFonts.available())return
    setPhase('loading')
    void localFonts.list().then(families=>{if(!cancelled){setRows(families);setPhase('ready')}}).catch(()=>{if(!cancelled)setPhase('failed')})
    return()=>{cancelled=true}
  },[attempt])
  useEffect(()=>{
    let cancelled=false
    setUsable(true)
    if(isLocalFont(value)&&phase==='ready')void fontAvailable(value).then(ok=>{if(!cancelled)setUsable(ok)})
    return()=>{cancelled=true}
  },[value,phase])
  const name=(font:string)=>localFontName(font)??(font==='system'?(systemLabel??tr('跟随系统')):font==='system-mono'?systemMonoName()?tr('{p0}（系统）',systemMonoName()):tr('系统等宽字体'):font==='Maple Mono CN'?'Maple Mono CJK':font)
  const choices=useMemo(()=>{
    const selectedName=localFontName(value)?.toLowerCase()
    const local=rows.filter(row=>!monospaced||showAll||row.monospaced||row.name.toLowerCase()===selectedName).map(row=>({value:row.name.toLowerCase()===selectedName?value:localFontId(row.name),label:row.name,description:row.monospaced?tr('本机字体 · 等宽'):tr('本机字体')}))
    const missing=isLocalFont(value)&&!rows.some(row=>row.name.toLowerCase()===localFontName(value)!.toLowerCase())
    return [
      {type:'section' as const,title:tr('默认与内置'),options:[...options].sort((a,b)=>name(a).localeCompare(name(b),language)).map(font=>({value:font,label:font===defaultValue?tr('{p0}（默认）',name(font)):name(font),description:isSystemFont(font)?tr('由系统提供'):tr('内置 · 离线可用')}))},
      ...(missing?[{type:'section' as const,title:tr('已保存的选择'),options:[{value,label:name(value),description:phase==='loading'?tr('正在读取本机字体…'):phase==='failed'||!localFonts.available()?tr('此字体在当前设备不可用，暂用默认字体。'):tr('本机未安装，暂用默认字体'),disabled:true}]}]:[]),
      ...(local.length?[{type:'section' as const,title:tr('本机已安装'),options:local}]:[]),
    ]
  },[rows,monospaced,showAll,value,options,defaultValue,systemLabel,language,phase])
  const status=error?{type:'error' as const,message:localizeMessage(error)}:phase==='failed'?{type:'warning' as const,message:tr('无法读取本机字体，仍可使用内置字体。')}:!usable?{type:'warning' as const,message:tr('此字体在当前设备不可用，暂用默认字体。')}:undefined
  return <Stack gap={1} width="100%">
    <Selector id={id} label={label} isLabelHidden size="md" width="100%" value={value} isDisabled={busy}
      hasSearch searchPlaceholder={tr('搜索字体…')} emptySearchText={tr('没有匹配的字体')} options={choices}
      isLoading={phase==='loading'} status={status} statusVariant="detached"
      onChange={async font=>{setError('');setBusy(true);try{const ok=await fontAvailable(font);if(!active.current)return;if(ok)currentChange.current(font);else setError(tr('无法加载 {p0}，已保留当前字体。',name(font)))}finally{if(active.current)setBusy(false)}}}/>
    {monospaced&&rows.length>0&&<Switch id={id+'-all'} size="sm" label={tr('显示全部字体')} value={showAll} onChange={setShowAll}/>}
    {phase==='failed'&&<Button size="sm" variant="ghost" label={tr('重新读取字体')} onClick={()=>setAttempt(n=>n+1)}/>}
  </Stack>
}
