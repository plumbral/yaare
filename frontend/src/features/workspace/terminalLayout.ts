import { workspaceReducer, type Action, type HostProfile, type Session, type WorkspaceState } from './mockGateway'

export const maximumPanes = 8
export const paneMinimum = { width: 320, height: 200 }
export type SplitAxis = 'horizontal' | 'vertical'
export type SplitNode = { kind: 'leaf'; id: string } | { kind: 'split'; id: string; axis: SplitAxis; ratio: number; first: SplitNode; second: SplitNode }
export interface TerminalGroup { workspacePinned?: boolean; workspaceId?: string; workspaceName?: string; workspaceHostId?: string; customName?: boolean; id: string; name: string; root: SplitNode; activeId: string; maximized: boolean; pinned: boolean }
export interface PaneSession extends Session { pending?: boolean; routeSourceId?: string; label?:string }
export interface TerminalWorkspace extends WorkspaceState { sessions: PaneSession[]; groups: TerminalGroup[] }
export const presetSizes = { side: 2, stack: 2, three: 3, four: 4 } as const
export type SplitPreset = keyof typeof presetSizes
export type DockSide = 'left' | 'right' | 'top' | 'bottom'
export type LayoutAction = Action
  | {type:'dock'; source:string; wholeTab:boolean; target:string; side:DockSide; nodeId:string}
  | {type:'renamePane'; id:string; name:string}
  | {type:'orderTab'; id:string; target:string; after:boolean}
  | {type:'regroup'; groupIds:string[]; workspaceId:string; name:string}
  | {type:'preset'; groupIds:string[]; paneOrder:string[]; preset:SplitPreset; token:string}
  | { type: 'workspaceTab'; id: string; host: HostProfile; parentGroupId: string }
  | { type: 'updateSessionHost'; id: string; host: HostProfile }
  | { type: 'restore'; workspace: TerminalWorkspace }
  | { type: 'split'; id: string; newId: string; nodeId: string; axis: SplitAxis }
  | { type: 'attach'; id: string; host: HostProfile }
  | { type: 'move'; id: string; target: string }
  | { type: 'detach'; id: string; groupId: string }
  | { type: 'swap'; id: string; target: string }
  | { type: 'ratio'; groupId: string; nodeId: string; ratio: number }
  | { type: 'arrange'; groupId: string; columns: number }
  | { type: 'maximize'; groupId: string }
  | { type: 'pinWorkspace'; workspaceId:string; pinned:boolean }
  | { type: 'pinGroup'; groupId: string; pinned: boolean }
  | { type: 'renameGroup'; groupId: string; name: string }

export const leaves = (node: SplitNode): string[] => node.kind === 'leaf' ? [node.id] : [...leaves(node.first), ...leaves(node.second)]
export const groupFor = (state: Pick<TerminalWorkspace,'groups'>, id: string) => state.groups.find(g => leaves(g.root).includes(id))
export function hidePaneView<Value extends string | boolean>(views: Record<string, Value>, id: string) {
  return views[id] ? { ...views, [id]: false as const } : views
}
export function setPaneView<Value extends string | boolean>(views: Record<string, Value>, id: string, value: Value): Record<string, Value> {
  return views[id] === value ? views : { ...views, [id]: value }
}
const leaf = (id: string): SplitNode => ({ kind: 'leaf', id })
function mapNode(node: SplitNode, visit: (node: SplitNode) => SplitNode): SplitNode {
  return visit(node.kind === 'leaf' ? node : { ...node, first: mapNode(node.first, visit), second: mapNode(node.second, visit) })
}
function removeNode(node: SplitNode, id: string): SplitNode | null {
  if (node.kind === 'leaf') return node.id === id ? null : node
  const first=removeNode(node.first,id),second=removeNode(node.second,id)
  return first && second ? {...node,first,second} : first ?? second
}
function removeFromGroups(groups: TerminalGroup[], id: string): TerminalGroup[] {
  return groups.flatMap(g=>{if(!leaves(g.root).includes(id))return [g];const root=removeNode(g.root,id);if(!root)return [];return [{...g,root,activeId:g.activeId===id?leaves(root)[0]!:g.activeId,maximized:leaves(root).length>1&&g.maximized}]})
}
function activate(state: TerminalWorkspace,id:string):TerminalWorkspace {
  if(!state.sessions.some(s=>s.id===id))return state
  const group=groupFor(state,id)
  const groupChanged=!!group&&group.activeId!==id
  if(state.activeId===id&&!groupChanged)return state
  return {...state,activeId:id,groups:groupChanged?state.groups.map(g=>g===group?{...g,activeId:id}:g):state.groups}
}
export function terminalWorkspaceReducer(state: TerminalWorkspace,action:LayoutAction):TerminalWorkspace {
  switch(action.type){
    case 'orderTab': {
      const source=state.groups.find(g=>g.id===action.id),target=state.groups.find(g=>g.id===action.target)
      if(!source||!target||source===target)return state
      const groups=state.groups.filter(g=>g!==source)
      groups.splice(groups.indexOf(target)+(action.after?1:0),0,source)
      return {...state,groups}
    }
    case 'regroup': {
      const {groupIds,workspaceId,name}=action
      if(!name.trim()||name.length>64||!groupIds.length)return state
      return {...state,groups:state.groups.map(g=>groupIds.includes(g.id)?{...g,workspaceId,workspaceName:name.trim()}:g)}
    }
    case 'dock': {
      const source=groupFor(state,action.source),target=groupFor(state,action.target)
      if(!source||!target||action.source===action.target||source===target&&action.wholeTab)return state
      const moving=action.wholeTab?source.root:leaf(action.source)
      if(source!==target&&leaves(target.root).length+leaves(moving).length>maximumPanes)return state
      const remaining=action.wholeTab?state.groups.filter(g=>g!==source):removeFromGroups(state.groups,action.source)
      const before=action.side==='left'||action.side==='top'
      const axis=action.side==='left'||action.side==='right'?'horizontal':'vertical'
      const groups=remaining.map(g=>g.id!==target.id?g:{...g,maximized:false,activeId:action.source,
        root:mapNode(g.root,n=>n.kind==='leaf'&&n.id===action.target?
          {kind:'split',id:action.nodeId,axis,ratio:.5,first:before?moving:n,second:before?n:moving}:n)})
      return {...state,groups,activeId:action.source}
    }
    case 'preset': {
      const {groupIds,paneOrder,preset,token}=action
      const selected=groupIds.map(id=>state.groups.find(g=>g.id===id))
      const base=selected[0]
      if(!base||selected.some(g=>!g)||new Set(groupIds).size!==groupIds.length)return state
      const original=selected.flatMap(g=>leaves(g!.root))
      if(original.length!==paneOrder.length||new Set(paneOrder).size!==original.length||original.some(id=>!paneOrder.includes(id)))return state
      const count=presetSizes[preset], slots=paneOrder.slice(0,count)
      const source=state.sessions.find(s=>s.id===base.activeId)
      if(!source)return state
      const sessions=[...state.sessions]
      while(slots.length<count){
        const id=token+'p'+slots.length
        if(sessions.some(s=>s.id===id))return state
        sessions.push({id,host:{...source.host},pending:true,routeSourceId:source.id})
        slots.push(id)
      }
      const activeId=slots.includes(state.activeId)?state.activeId:slots[0]!
      const main={...base,activeId,maximized:false,root:presetTree(preset,slots,token)}
      const overflow=paneOrder.slice(count).map((id,i)=>{
        const owner=groupFor(state,id)!
        const pane=state.sessions.find(s=>s.id===id)!
        return {...owner,id:token+'t'+i,name:pane.label??pane.host.name,customName:!!pane.label,activeId:id,maximized:false,root:leaf(id)}
      })
      return {...state,activeId,sessions:sessions.length===state.sessions.length?state.sessions:sessions,
        groups:state.groups.flatMap(g=>g===base?[main,...overflow]:groupIds.includes(g.id)?[]:[g])}
    }
    case 'updateSessionHost': return {...state,sessions:state.sessions.map(session=>session.id===action.id?{...session,host:action.host}:session)}
    case 'restore': return state.sessions.length?state:action.workspace
    case 'activate': return activate(state,action.id)
    case 'open':case 'connect':case 'workspaceTab': {
      const parent=action.type==='workspaceTab'?state.groups.find(group=>group.id===action.parentGroupId):undefined
      if(action.type==='workspaceTab'&&!parent)return state
      const existing=action.type==='open'?state.sessions.find(s=>!s.pending&&s.host.id===action.host.id&&groupFor(state,s.id)?.workspaceHostId===action.host.id):undefined
      if(existing)return activate(state,existing.id)
      if(state.sessions.some(s=>s.id===action.id))return state
      const base=workspaceReducer(state,{type:'connect',id:action.id,host:action.host})
      return {...base,groups:[...state.groups,{id:action.id,workspacePinned:parent?.workspacePinned??false,workspaceId:parent?.workspaceId??parent?.id??action.id,workspaceName:parent?.workspaceName??parent?.name??action.host.name,workspaceHostId:parent?.workspaceHostId??action.host.id,name:action.host.name,root:leaf(action.id),activeId:action.id,maximized:false,pinned:false}]}
    }
    case 'close': {
      const group=groupFor(state,action.id),index=state.groups.findIndex(g=>g===group),groups=removeFromGroups(state.groups,action.id)
      const next=groups.find(g=>g.id===group?.id)??groups.find(g=>g.workspaceId&&g.workspaceId===group?.workspaceId)??groups[Math.max(0,index-1)]
      return {...state,sessions:state.sessions.filter(s=>s.id!==action.id),groups,activeId:state.activeId===action.id?next?.activeId??'':state.activeId}
    }
    case 'split': {
      const group=groupFor(state,action.id),source=state.sessions.find(s=>s.id===action.id)
      if(!group||!source||source.pending||leaves(group.root).length>=maximumPanes||state.sessions.some(s=>s.id===action.newId))return state
      const root=mapNode(group.root,node=>node.kind==='leaf'&&node.id===action.id?{kind:'split',id:action.nodeId,axis:action.axis,ratio:.5,first:node,second:leaf(action.newId)}:node)
      return {...state,activeId:action.newId,sessions:[...state.sessions,{id:action.newId,host:{...source.host},pending:true,routeSourceId:source.id}],groups:state.groups.map(g=>g===group?{...g,root,activeId:action.newId,maximized:false}:g)}
    }
    case 'attach': return {...state,sessions:state.sessions.map(s=>s.id===action.id&&s.pending?{...s,pending:false,routeSourceId:undefined,host:{...action.host}}:s)}
    case 'maximize': return {...state,groups:state.groups.map(g=>g.id===action.groupId?{...g,maximized:!g.maximized}:g)}
    case 'pinWorkspace': return {...state,groups:state.groups.map(g=>(g.workspaceId??g.id)===action.workspaceId?{...g,workspacePinned:action.pinned}:g)}
    case 'pinGroup': {
      const group=state.groups.find(g=>g.id===action.groupId)
      if(!group||!!(group.pinned||group.workspacePinned)===action.pinned)return state
      return {...state,groups:state.groups.map(g=>g===group?{...g,pinned:action.pinned,workspacePinned:false}:g)}
    }
    case 'renameGroup': case 'renamePane': {
      const name=action.name.trim()
      const group=action.type==='renameGroup'?state.groups.find(g=>g.id===action.groupId):groupFor(state,action.id)
      if(!group||!name||name.length>64||/[\u0000-\u001f\u007f]/.test(name))return state
      const paneId=action.type==='renamePane'?action.id:group.root.kind==='leaf'?group.root.id:undefined
      return {...state,groups:action.type==='renameGroup'||group.root.kind==='leaf'?state.groups.map(g=>g===group?{...g,name,customName:true}:g):state.groups,
        sessions:paneId?state.sessions.map(p=>p.id===paneId?{...p,label:name}:p):state.sessions}
    }
    case 'ratio': return Number.isFinite(action.ratio)?{...state,groups:state.groups.map(g=>g.id===action.groupId?{...g,root:resizeSplit(g.root, action.nodeId, action.ratio)}:g)}:state
    case 'arrange': {
      const group=state.groups.find(g=>g.id===action.groupId);if(!group||!Number.isInteger(action.columns)||action.columns<1||action.columns>8)return state
      const ids=leaves(group.root);const rows:string[][]=[];for(let i=0;i<ids.length;i+=action.columns)rows.push(ids.slice(i,i+action.columns))
      const chain=(nodes:SplitNode[],axis:SplitAxis,key:string):SplitNode=>nodes.length===1?nodes[0]!:{kind:'split',id:key,axis,ratio:1/nodes.length,first:nodes[0]!,second:chain(nodes.slice(1),axis,key+'b')}
      const root=chain(rows.map((row,i)=>chain(row.map(leaf),'horizontal',group.id+'row'+i)),'vertical',group.id+'rows')
      return {...state,groups:state.groups.map(g=>g===group?{...g,root,maximized:false}:g)}
    }
    case 'move': {
      const source=state.sessions.find(s=>s.id===action.id),target=state.sessions.find(s=>s.id===action.target),group=groupFor(state,action.target)
      if(!source||source.pending||!target?.pending||!group||action.id===action.target)return state
      const groups=removeFromGroups(state.groups,action.id).map(g=>g.id===group.id?{...g,root:mapNode(g.root,n=>n.kind==='leaf'&&n.id===action.target?leaf(action.id):n),activeId:action.id}:g)
      return {...state,sessions:state.sessions.filter(s=>s.id!==action.target),groups,activeId:action.id}
    }
    case 'detach': {
      const source=state.sessions.find(s=>s.id===action.id),group=groupFor(state,action.id)
      if(!source||source.pending||!group||leaves(group.root).length===1||state.groups.some(g=>g.id===action.groupId))return state
      return {...state,groups:[...removeFromGroups(state.groups,action.id),{id:action.groupId,workspacePinned:false,workspaceId:action.groupId,workspaceHostId:source.host.id,workspaceName:source.host.name,name:source.label??source.host.name,customName:!!source.label,root:leaf(action.id),activeId:action.id,maximized:false,pinned:false}],activeId:action.id}
    }
    case 'swap': {
      if(action.id===action.target||!state.sessions.some(s=>s.id===action.id)||!state.sessions.some(s=>s.id===action.target))return state
      return activate({...state,groups:state.groups.map(g=>({...g,activeId:g.activeId===action.id?action.target:g.activeId===action.target?action.id:g.activeId,root:mapNode(g.root,n=>n.kind==='leaf'?leaf(n.id===action.id?action.target:n.id===action.target?action.id:n.id):n)}))},action.id)
    }
    default: return {...workspaceReducer(state,action),groups:state.groups}
  }
}

export function resizeSplit(root: SplitNode, id: string, ratio: number): SplitNode {
  return mapNode(root, node => node.kind === 'split' && node.id === id
    ? { ...node, ratio: Math.max(.05, Math.min(.95, ratio)) } : node)
}

export interface PaneRect { x:number; y:number; width:number; height:number }
export interface DividerRect { id:string; axis:SplitAxis; ratio:number; rect:PaneRect; firstMinimum:number; secondMinimum:number }
export function minimumSize(node:SplitNode):{width:number;height:number}{
  if(node.kind==='leaf')return paneMinimum
  const a=minimumSize(node.first),b=minimumSize(node.second)
  return node.axis==='horizontal'?{width:a.width+b.width+1,height:Math.max(a.height,b.height)}:{width:Math.max(a.width,b.width),height:a.height+b.height+1}
}
export function layoutRects(node:SplitNode,width:number,height:number){
  const panes=new Map<string,PaneRect>(),dividers:DividerRect[]=[]
  const visit=(n:SplitNode,r:PaneRect)=>{if(n.kind==='leaf'){panes.set(n.id,r);return};const a=minimumSize(n.first),b=minimumSize(n.second),horizontal=n.axis==='horizontal',length=Math.max(0,(horizontal?r.width:r.height)-1),minimumA=horizontal?a.width:a.height,minimumB=horizontal?b.width:b.height
    const first=Math.max(Math.min(minimumA,length),Math.min(Math.max(0,length-minimumB),Math.round(length*n.ratio)))
    const second=Math.max(0,length-first)
    dividers.push({id:n.id,axis:n.axis,ratio:length?first/length:.5,rect:r,firstMinimum:minimumA,secondMinimum:minimumB})
    visit(n.first,{...r,width:horizontal?first:r.width,height:horizontal?r.height:first});visit(n.second,{x:r.x+(horizontal?first+1:0),y:r.y+(horizontal?0:first+1),width:horizontal?second:r.width,height:horizontal?r.height:second})
  };visit(node,{x:0,y:0,width:Math.max(0,width),height:Math.max(0,height)});return {panes,dividers}
}
export function canSplit(rect:PaneRect|undefined,axis:SplitAxis){return !!rect&&(axis==='horizontal'?rect.width>=paneMinimum.width*2+1:rect.height>=paneMinimum.height*2+1)}

export function presetTree(preset:SplitPreset,ids:readonly string[],key:string):SplitNode {
  const node=(i:number)=>leaf(ids[i]!)
  const split=(suffix:string,axis:SplitAxis,first:SplitNode,second:SplitNode):SplitNode=>
    ({kind:'split',id:key+suffix,axis,ratio:.5,first,second})
  if(preset==='three')return split('a','horizontal',node(0),split('b','vertical',node(1),node(2)))
  if(preset==='four')return split('a','vertical',split('b','horizontal',node(0),node(1)),split('c','horizontal',node(2),node(3)))
  return split('a',preset==='side'?'horizontal':'vertical',node(0),node(1))
}

export function dropZone(x:number,y:number,wholeTab:boolean):DockSide|'center' {
  const edges: [DockSide,number][]=[['left',x],['right',1-x],['top',y],['bottom',1-y]]
  const nearest=edges.reduce((a,b)=>a[1]<b[1]?a:b)
  return !wholeTab&&nearest[1]>.25?'center':nearest[0]
}
