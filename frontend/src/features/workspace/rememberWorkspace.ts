import { tr } from '../../shared/i18n/messages'
import type {SessionLayout,LayoutNode} from '../../shared/generated/mygo'
import {idleConnection} from './sessionConnection'
import {leaves,type SplitNode,type TerminalWorkspace} from './terminalLayout'
import type {HostProfile} from './mockGateway'
export function captureSessionLayout(runtime:TerminalWorkspace,hosts:HostProfile[],paths:Record<string,string>,files:Record<string,boolean>,startOpen:boolean,sizes:{start:number;end:number}):SessionLayout{
 const valid=new Set(hosts.map(h=>h.id));let count=0
 const groups=runtime.groups.slice(0,16).filter(g=>{count+=leaves(g.root).length;return count<=64}).map(g=>({...g,root:copyNode(g.root)}))
 const ids=new Set(groups.flatMap(g=>leaves(g.root as SplitNode)))
 const panes=runtime.sessions.filter(s=>ids.has(s.id)).map(s=>({id:s.id,label:s.label,hostId:valid.has(s.host.id)?s.host.id:'',pending:!!s.pending||!valid.has(s.host.id),path:paths[s.id]??'.',filesOpen:files[s.id]??false}))
 const activeId=ids.has(runtime.activeId)?runtime.activeId:groups[0]?.activeId??''
 return {version:0,format:1,enabled:true,groups,panes,activeId,view:'hosts',startOpen,startWidth:Math.round(Math.max(200,Math.min(800,sizes.start))),endWidth:Math.round(Math.max(280,Math.min(1000,sizes.end)))}
}
function copyNode(n:SplitNode):LayoutNode{return n.kind==='leaf'?{kind:'leaf',id:n.id}:{kind:'split',id:n.id,axis:n.axis,ratio:n.ratio,first:copyNode(n.first),second:copyNode(n.second)}}
function restoreNode(n:LayoutNode):SplitNode{return n.kind==='leaf'?{kind:'leaf',id:n.id}:{kind:'split',id:n.id,axis:n.axis==='vertical'?'vertical':'horizontal',ratio:n.ratio!,first:restoreNode(n.first!),second:restoreNode(n.second!)}}
export function restoreSessionLayout(saved:SessionLayout,hosts:HostProfile[]):TerminalWorkspace{
 const byId=new Map(hosts.map(h=>[h.id,h]))
 const savedPanes=new Map(saved.panes.map(p=>[p.id,p]))
 const groups=saved.groups.map(g=>{
   const root=restoreNode(g.root)
   const firstHost=savedPanes.get(leaves(root)[0]!)?.hostId
   return {...g,root,workspacePinned:g.workspaceId?!!g.workspacePinned:g.pinned,workspaceId:g.workspaceId??g.id,workspaceHostId:g.workspaceHostId??firstHost,workspaceName:g.workspaceName??(g.customName?byId.get(firstHost??'')?.name:undefined)??g.name}
 })
 return {hosts,sessions:saved.panes.map(p=>{const host=byId.get(p.hostId);const group=groups.find(g=>g.customName&&g.root.kind==='leaf'&&g.root.id===p.id);return{id:p.id,label:p.label??group?.name,host:host?{...host}:{id:'',name:tr("未选择主机"),address:'',port:22,username:'',group:'未分组',system:'Linux'},pending:p.pending||!host,connection:{...idleConnection}}}),activeId:saved.activeId,groups}
}
