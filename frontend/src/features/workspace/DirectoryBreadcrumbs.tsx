import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import {Breadcrumbs,BreadcrumbItem} from '@astryxdesign/core/Breadcrumbs'
import {directoryCrumbs} from './directoryHistory'

export default function DirectoryBreadcrumbs({path,label,rootLabel=tr("根目录"),disabled,onNavigate}:{path:string;label:string;rootLabel?:string;disabled:boolean;onNavigate:(path:string)=>void}){
  useLocale()

 const items=directoryCrumbs(path,rootLabel),collapsed=items.length>4,hidden=collapsed?items.slice(1,-2):[]
 const crumb=(item:typeof items[number],current=false)=><BreadcrumbItem key={item.path} href={disabled?undefined:'#'} isCurrent={current} onClick={event=>{event.preventDefault();if(!disabled)onNavigate(item.path)}}>{item.label}</BreadcrumbItem>
 return <div style={{display:'flex',alignItems:'center',minHeight:'var(--spacing-8)',minWidth:0,overflowX:'auto',scrollbarWidth:'thin',paddingInline:'var(--spacing-1)'}}><Breadcrumbs label={label} variant="supporting">
  {crumb(items[0]!,items.length===1)}
  {collapsed&&<BreadcrumbItem isCurrent={false} menu={hidden.map(item=>({label:item.label,isDisabled:disabled,onClick:()=>onNavigate(item.path)}))}>…</BreadcrumbItem>}
  {(collapsed?items.slice(-2):items.slice(1)).map((item,index,array)=>crumb(item,index===array.length-1))}
 </Breadcrumbs></div>
}
