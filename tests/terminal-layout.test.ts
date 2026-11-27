import { expect,test } from 'bun:test'
import { initialHosts } from '../frontend/src/features/workspace/mockGateway'
import { indexWorkspaces } from '../frontend/src/features/workspace/workspaceNavigation'
import { terminalWorkspaceReducer as reduce,leaves,groupFor,layoutRects,minimumSize,canSplit,resizeSplit,type TerminalWorkspace,type SplitNode } from '../frontend/src/features/workspace/terminalLayout'
const empty=():TerminalWorkspace=>({hosts:[],sessions:[],groups:[],activeId:''})
function initial(){return reduce(empty(),{type:'connect',id:'one',host:initialHosts[0]!})}

test('independent and split connections to the same host stay in their own workspaces',()=>{
  const other={...initialHosts[0]!,id:'kfc',name:'KFC'}
  let state=reduce(initial(),{type:'split',id:'one',newId:'inside',nodeId:'split',axis:'vertical'})
  state=reduce(state,{type:'attach',id:'inside',host:other})
  state=reduce(state,{type:'open',id:'independent',host:other})
  state=reduce(state,{type:'workspaceTab',id:'secondTab',parentGroupId:'one',host:other})
  const index=()=>indexWorkspaces(state.groups,new Map(state.sessions.map(s=>[s.id,s])))
  expect([...index().keys()]).toEqual(['one','independent'])
  expect(index().get('one')!.groups.map(g=>g.id)).toEqual(['one','secondTab'])
  expect(index().get('independent')!.panes.map(p=>p.id)).toEqual(['independent'])
  state=reduce(state,{type:'close',id:'one'})
  expect(index().get('one')!.name).toBe(initialHosts[0]!.name)
  for(const pane of index().get('one')!.panes)state=reduce(state,{type:'close',id:pane.id})
  expect(state.sessions.map(s=>s.id)).toEqual(['independent'])
  expect(state.activeId).toBe('independent')
})
function check(state:TerminalWorkspace){const seen=state.groups.flatMap(g=>leaves(g.root));expect(new Set(seen).size).toBe(seen.length);expect([...seen].sort()).toEqual(state.sessions.map(s=>s.id).sort());for(const group of state.groups){expect(leaves(group.root).length).toBeLessThanOrEqual(8);expect(leaves(group.root)).toContain(group.activeId)}if(state.activeId)expect(seen).toContain(state.activeId)}
test('split limits apply per tab, including pending panes, and same host creates independent sessions',()=>{let state=initial();for(let i=2;i<=8;i++){state=reduce(state,{type:'split',id:'one',newId:'p'+i,nodeId:'n'+i,axis:'horizontal'});state=reduce(state,{type:'attach',id:'p'+i,host:initialHosts[0]!});check(state)}expect(state.groups).toHaveLength(1);expect(state.sessions).toHaveLength(8);const previous=state;expect(reduce(state,{type:'split',id:'one',newId:'ninth',nodeId:'overflow',axis:'vertical'})).toBe(previous);state=reduce(state,{type:'connect',id:'other',host:initialHosts[1]!});expect(state.groups).toHaveLength(2);expect(state.sessions).toHaveLength(9);check(state)})
test('moving a live session preserves its object and removes only the empty source tab',()=>{let state=initial();state=reduce(state,{type:'split',id:'one',newId:'slot',nodeId:'s',axis:'horizontal'});state=reduce(state,{type:'connect',id:'live',host:initialHosts[1]!});const session=state.sessions.find(s=>s.id==='live')!;state=reduce(state,{type:'move',id:'live',target:'slot'});expect(state.groups).toHaveLength(1);expect(state.sessions.find(s=>s.id==='live')).toBe(session);expect(state.activeId).toBe('live');expect(leaves(state.groups[0]!.root)).toEqual(['one','live']);check(state);state=reduce(state,{type:'detach',id:'live',groupId:'detached'});expect(state.groups).toHaveLength(2);expect(state.sessions.find(s=>s.id==='live')).toBe(session);check(state)})
test('closing collapses branches, preserves active neighbors, and cannot revive a closed pane',()=>{let state=initial();state=reduce(state,{type:'split',id:'one',newId:'two',nodeId:'h',axis:'horizontal'});state=reduce(state,{type:'attach',id:'two',host:initialHosts[0]!});state=reduce(state,{type:'split',id:'two',newId:'three',nodeId:'v',axis:'vertical'});state=reduce(state,{type:'close',id:'two'});expect(leaves(state.groups[0]!.root)).toEqual(['one','three']);expect(state.activeId).toBe('three');check(state);state=reduce(state,{type:'close',id:'three'});expect(state.groups[0]!.root).toEqual({kind:'leaf',id:'one'});state=reduce(state,{type:'close',id:'one'});check(state);expect(state.groups).toHaveLength(0);expect(state.activeId).toBe('');expect(reduce(state,{type:'activate',id:'three'})).toBe(state)})
test('layouts and maximize retain session identities, clamp resize floors, and never overlap',()=>{let state=initial();for(let i=2;i<=8;i++){state=reduce(state,{type:'split',id:'one',newId:'p'+i,nodeId:'n'+i,axis:'horizontal'});state=reduce(state,{type:'attach',id:'p'+i,host:initialHosts[0]!})}const sessions=state.sessions;for(const columns of [1,2,3,4]){state=reduce(state,{type:'arrange',groupId:'one',columns});expect(state.sessions).toBe(sessions);const minimum=minimumSize(state.groups[0]!.root);for(const extra of [0,200,777]){const {panes,dividers}=layoutRects(state.groups[0]!.root,minimum.width+extra,minimum.height+extra);expect(panes.size).toBe(8);expect(dividers.length).toBe(7);for(const rect of panes.values()){expect(rect.width).toBeGreaterThanOrEqual(320);expect(rect.height).toBeGreaterThanOrEqual(200)}const values=[...panes.values()];for(let i=0;i<values.length;i++)for(let j=i+1;j<values.length;j++){const a=values[i]!,b=values[j]!;expect(a.x+a.width<=b.x||b.x+b.width<=a.x||a.y+a.height<=b.y||b.y+b.height<=a.y).toBe(true)}}check(state)}const root=state.groups[0]!.root;state=reduce(state,{type:'maximize',groupId:'one'});state=reduce(state,{type:'maximize',groupId:'one'});expect(state.groups[0]!.root).toBe(root);expect(state.sessions).toBe(sessions);expect(canSplit({x:0,y:0,width:640,height:400},'horizontal')).toBe(false);expect(canSplit({x:0,y:0,width:641,height:400},'horizontal')).toBe(true)})
test('swap across groups preserves valid active panes and ratio rejects nonfinite input',()=>{let state=initial();state=reduce(state,{type:'connect',id:'other',host:initialHosts[1]!});state=reduce(state,{type:'swap',id:'one',target:'other'});expect(groupFor(state,'one')!.id).toBe('other');check(state);expect(reduce(state,{type:'ratio',groupId:'one',nodeId:'invalid',ratio:NaN})).toBe(state)})

test('live split preview and commit produce the same nested geometry', () => {
  let state = initial()
  state = reduce(state, { type: 'split', id: 'one', newId: 'two', nodeId: 'outer', axis: 'horizontal' })
  state = reduce(state, { type: 'attach', id: 'two', host: initialHosts[0]! })
  state = reduce(state, { type: 'split', id: 'two', newId: 'three', nodeId: 'inner', axis: 'vertical' })
  const root = state.groups[0]!.root
  for (const ratio of [0.01, 0.36, 0.98]) {
    const preview = layoutRects(resizeSplit(root, 'outer', ratio), 1200, 840)
    const committed = reduce(state, { type: 'ratio', groupId: 'one', nodeId: 'outer', ratio })
    expect(preview).toEqual(layoutRects(committed.groups[0]!.root, 1200, 840))
    expect(committed.sessions).toBe(state.sessions)
    expect(state.groups[0]!.root).toBe(root)
  }
})

test('docking a split tab preserves both live sessions and the split geometry',()=>{
  let state=reduce(initial(),{type:'split',id:'one',newId:'two',nodeId:'n',axis:'vertical'})
  state=reduce(state,{type:'connect',id:'three',host:initialHosts[1]!})
  const sessions=state.sessions,tree=state.groups[0]!.root
  state=reduce(state,{type:'dock',source:'two',target:'three',side:'left',wholeTab:true,nodeId:'dock'})
  check(state)
  expect(state.sessions).toBe(sessions)
  expect(state.groups[0]!.root).toMatchObject({axis:'horizontal',first:tree})
  state=reduce(state,{type:'dock',source:'three',target:'one',side:'bottom',wholeTab:false,nodeId:'again'})
  expect(state.activeId).toBe('three')
  check(state)
})

test('preset preview order, spare tabs, vacant slots and stale selections',()=>{
  let state=initial()
  for(const id of ['two','three','four'])state=reduce(state,{type:'connect',id,host:initialHosts[1]!})
  const sessions=state.sessions
  const action={type:'preset' as const,groupIds:state.groups.map(g=>g.id),paneOrder:['two','one','three','four'],preset:'three' as const,token:'layout'}
  state=reduce(state,action)
  expect(state.sessions).toBe(sessions)
  expect(leaves(state.groups[0]!.root)).toEqual(['two','one','three'])
  expect(state.groups[0]!.root).toMatchObject({axis:'horizontal',second:{axis:'vertical'}})
  expect(state.groups[1]!.activeId).toBe('four')
  expect(reduce(state,action)).toBe(state)
  state=reduce(state,{...action,groupIds:['one'],paneOrder:['two','one','three'],preset:'four',token:'new'})
  expect(state.sessions.at(-1)?.pending).toBe(true)
  check(state)
})

test('grouping and ordering tabs do not move the active shell or change its connection owner',()=>{
  const original=reduce(initial(),{type:'connect',id:'other',host:initialHosts[1]!})
  let grouped=reduce(original,{type:'regroup',groupIds:['one','other'],workspaceId:'review',name:'Review'})
  grouped=reduce(grouped,{type:'orderTab',id:'other',target:'one',after:false})
  expect(grouped.groups.map(g=>g.id)).toEqual(['other','one'])
  expect(grouped.groups.map(g=>g.workspaceId)).toEqual(['review','review'])
  expect(grouped.groups[0]!.workspaceHostId).toBe(initialHosts[1]!.id)
  expect(grouped.sessions).toBe(original.sessions)
  expect(grouped.activeId).toBe(original.activeId)
  const pinned=reduce(grouped,{type:'pinWorkspace',workspaceId:'review',pinned:true})
  const unpinned=reduce(pinned,{type:'pinGroup',groupId:'one',pinned:false})
  expect(unpinned.groups[0]!.workspacePinned).toBe(true)
  expect(unpinned.groups[1]!.workspacePinned).toBe(false)
})
