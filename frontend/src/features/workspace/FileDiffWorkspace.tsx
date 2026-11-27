import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import {lazy,Suspense,useEffect,useRef,useState} from 'react'
import {Stack,StackItem} from '@astryxdesign/core/Stack'
import {Text} from '@astryxdesign/core/Text'
import {Button} from '@astryxdesign/core/Button'
import {Selector} from '@astryxdesign/core/Selector'
import {fileGateway,type ComparedText} from '../../shared/bridge/files'
const DiffEditor=lazy(()=>import('./FileDiffEditor'))
export default function FileDiffWorkspace({request,active,onClose,gateway=fileGateway}:{request:{id:string;relative:string};active:boolean;onClose:()=>void;gateway?:typeof fileGateway}){
  useLocale()

 const [data,setData]=useState<ComparedText|null>(null),[left,setLeft]=useState('auto'),[right,setRight]=useState('auto'),[busy,setBusy]=useState(false),[error,setError]=useState(''),[reload,setReload]=useState(0),seq=useRef(0)
 useEffect(()=>{const version=++seq.current;setBusy(true);setError('');setData(null);void gateway.comparisonText(request.id,request.relative,left,right).then(result=>{if(seq.current===version)setData(result)}).catch(cause=>{if(seq.current===version)setError(cause instanceof Error?cause.message:tr("文本对比未能读取，请重新比较。"))}).finally(()=>{if(seq.current===version)setBusy(false)});return()=>{seq.current++}},[request.id,request.relative,reload])
 useEffect(()=>()=>{void gateway.forgetComparison(request.id).catch(()=>{})},[request.id,gateway])
 const options=['auto','UTF-8','UTF-16LE','UTF-16BE','GB18030','GBK'].map(value=>({value,label:value==='auto'?tr("自动"):value}))
 return <Stack role="region" aria-label={tr("本地与远程文本差异")} hidden={!active} inert={!active} style={{display:active?'flex':'none',height:'100%',minHeight:0,minWidth:0}}>
  <Stack padding={3} gap={2}><Stack direction="horizontal" align="center" gap={2} wrap="wrap"><StackItem size="fill"><Text type="label">{tr("只读文本差异 · ")}{request.relative==='.'?tr("当前文件"):request.relative}</Text></StackItem><Button label={tr("关闭对比")} variant="ghost" size="md" onClick={onClose}/></Stack>
   <Text type="supporting" color="secondary">{tr("宽窗口左右对照；窄窗格逐段显示。删除部分属于本地，新增部分属于远程；不会写入文件。")}</Text>
   <Stack direction="horizontal" align="end" gap={2} wrap="wrap"><Stack style={{flex:'1 1 calc(var(--spacing-10) * 3)',minWidth:0}}><Selector label={tr("本地读取编码")} size="md" value={left} options={options} onChange={setLeft}/></Stack><Stack style={{flex:'1 1 calc(var(--spacing-10) * 3)',minWidth:0}}><Selector label={tr("远程读取编码")} size="md" value={right} options={options} onChange={setRight}/></Stack><Button label={tr("重新读取对比")} size="md" variant="secondary" isDisabled={busy} onClick={()=>setReload(n=>n+1)}/></Stack>
  </Stack>
  <StackItem size="fill" style={{minHeight:0,minWidth:0,padding:'var(--spacing-3)'}}>{data?<Suspense fallback={<Text type="supporting">{tr("正在显示文本差异…")}</Text>}><DiffEditor left={data.left.content} right={data.right.content} path={data.right.path||data.left.path}/></Suspense>:<Text type="supporting" role={error?'alert':'status'}>{error||(busy?tr("正在读取两端快照…"):tr("请重新比较文件。"))}</Text>}</StackItem>
  {data&&<Stack padding={3}><Text type="supporting" color="secondary">{tr("本地 ")}{data.left.encoding} · {data.left.lineEnding} {tr(" ↔ 远程 ")}{data.right.encoding} · {data.right.lineEnding} {tr(" · 比较快照")}</Text></Stack>}
 </Stack>
}
