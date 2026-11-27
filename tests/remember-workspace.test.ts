import {expect,test} from 'bun:test'
import {captureSessionLayout,restoreSessionLayout} from '../frontend/src/features/workspace/rememberWorkspace'
import {terminalWorkspaceReducer,type TerminalWorkspace} from '../frontend/src/features/workspace/terminalLayout'
import {mockGateway} from '../frontend/src/features/workspace/mockGateway'
test('workspace records only host references and layout, restoring idle independent panes',()=>{
 const host={...mockGateway.listHosts()[0]!,password:'must never save',privateKey:'secret material'}
 let state:TerminalWorkspace={hosts:[host],sessions:[],groups:[],activeId:''};state=terminalWorkspaceReducer(state,{type:'connect',host,id:'pane1'});state=terminalWorkspaceReducer(state,{type:'split',id:'pane1',newId:'pane2',nodeId:'split1',axis:'horizontal'});state=terminalWorkspaceReducer(state,{type:'ratio',groupId:'pane1',nodeId:'split1',ratio:.37})
 const saved=captureSessionLayout(state,[host],{pane1:'/srv'},{pane1:true},false,{start:245,end:340}),json=JSON.stringify(saved)
 for(const value of ['must never save','secret material','password','privateKey',host.address,'connection','transport'])expect(json.includes(value)).toBe(false)
 const restored=restoreSessionLayout(saved,[host]);expect(restored.sessions.length).toBe(2);expect(restored.sessions[0]!.connection?.status).toBe('idle');expect(restored.sessions[1]!.pending).toBe(true);expect(restored.groups[0]!.root).toEqual(state.groups[0]!.root)
 expect(terminalWorkspaceReducer(state,{type:'restore',workspace:restored})).toBe(state)
 const absent=restoreSessionLayout(saved,[]);expect(absent.sessions.every(s=>s.pending)).toBe(true);expect(absent.sessions[0]!.host.address).toBe('')
})
test('eight panes preserve their shape and active slot through a metadata round trip',()=>{
 const host=mockGateway.listHosts()[0]!;let state:TerminalWorkspace={hosts:[host],sessions:[],groups:[],activeId:''};state=terminalWorkspaceReducer(state,{type:'connect',host,id:'pane0'})
 for(let i=1;i<8;i++){state=terminalWorkspaceReducer(state,{type:'split',id:'pane0',newId:'pane'+i,nodeId:'split'+i,axis:i%2?'horizontal':'vertical'});state=terminalWorkspaceReducer(state,{type:'attach',id:'pane'+i,host})}
 const record=captureSessionLayout(state,[host],{}, {},true,{start:256,end:320})
 expect(record.view).toBe('hosts')
 for (const view of ['hosts','terminal']) {
   const restored = restoreSessionLayout({...record,view}, [host])
   expect(restored.sessions).toHaveLength(8)
   expect(restored.activeId).toBe(state.activeId)
   expect(restored.groups).toEqual(state.groups)
   expect(restored.sessions.every(s=>s.connection?.status==='idle')).toBe(true)
 }
})
test('pinning a split tab preserves the active session and survives layout restore without connecting',()=>{
 const host=mockGateway.listHosts()[0]!
 let state:TerminalWorkspace={hosts:[host],sessions:[],groups:[],activeId:''}
 state=terminalWorkspaceReducer(state,{type:'connect',host,id:'one'})
 state=terminalWorkspaceReducer(state,{type:'split',id:'one',newId:'split-pane',nodeId:'branch',axis:'horizontal'})
 state=terminalWorkspaceReducer(state,{type:'connect',host,id:'two'})
 const pinned=terminalWorkspaceReducer(state,{type:'pinGroup',groupId:'one',pinned:true})
 expect(pinned.activeId).toBe('two')
 expect(pinned.sessions).toBe(state.sessions)
 expect(pinned.groups[0]!.root).toBe(state.groups[0]!.root)
 expect(pinned.groups[1]).toBe(state.groups[1])
 const restored=restoreSessionLayout(captureSessionLayout(pinned,[host],{}, {},true,{start:256,end:320}),[host])
 expect(restored.groups.map(group=>group.pinned)).toEqual([true,false])
 expect(restored.sessions.every(session=>session.connection?.status==='idle')).toBe(true)
 const unpinned=terminalWorkspaceReducer(restored,{type:'pinGroup',groupId:'one',pinned:false})
 expect(unpinned.sessions).toBe(restored.sessions)
 expect(unpinned.groups[0]!.pinned).toBe(false)
})

test('restoring independent workspaces and scoped tabs supports layouts saved before workspace ownership',()=>{
 const [primary,other]=mockGateway.listHosts()
 let state:TerminalWorkspace={hosts:[primary!,other!],sessions:[],groups:[],activeId:''}
 state=terminalWorkspaceReducer(state,{type:'connect',host:primary!,id:'one'})
 state=terminalWorkspaceReducer(state,{type:'connect',host:other!,id:'two'})
 state=terminalWorkspaceReducer(state,{type:'pinWorkspace',workspaceId:'one',pinned:true})
 state=terminalWorkspaceReducer(state,{type:'workspaceTab',parentGroupId:'one',host:other!,id:'three'})
 const saved=captureSessionLayout(state,state.hosts,{}, {},true,{start:256,end:320})
 expect(restoreSessionLayout(saved,state.hosts).groups).toEqual(state.groups)
 const old={...saved,groups:saved.groups.map(({workspaceId,workspaceName,workspaceHostId,workspacePinned,...g})=>g)}
 expect(restoreSessionLayout(old,state.hosts).groups.map(g=>g.workspaceId)).toEqual(['one','two','three'])
})

test('pane names survive tab composition, detachment and a workspace restore',()=>{
 const hosts=mockGateway.listHosts().slice(0,2)
 let state:TerminalWorkspace={hosts,sessions:[],groups:[],activeId:''}
 for(const [i,host] of hosts.entries())state=terminalWorkspaceReducer(state,{type:'connect',id:'p'+i,host})
 state=terminalWorkspaceReducer(state,{type:'renameGroup',groupId:'p1',name:'Logs'})
 state=terminalWorkspaceReducer(state,{type:'dock',source:'p1',target:'p0',side:'right',wholeTab:true,nodeId:'n'})
 state=terminalWorkspaceReducer(state,{type:'renamePane',id:'p0',name:'Shell'})
 const saved=captureSessionLayout(state,hosts,{},{},true,{start:256,end:320})
 state=restoreSessionLayout(saved,hosts)
 expect(state.sessions.map(p=>p.label)).toEqual(['Shell','Logs'])
 state=terminalWorkspaceReducer(state,{type:'detach',id:'p1',groupId:'solo'})
 expect(state.groups.find(g=>g.id==='solo')?.name).toBe('Logs')
})
