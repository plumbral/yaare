import { expect, test } from 'bun:test'
import { forwardError, networkErrors, proxyError, emptyProxy } from '../frontend/src/features/connections/networkConfig'
import { previewChange, type SavedWorkspace } from '../frontend/src/features/workspace/savedWorkspace'
import type { HostProfile } from '../frontend/src/features/workspace/mockGateway'
import type { ForwardRule } from '../frontend/src/shared/generated/mygo'

const a:HostProfile={id:'a',name:'Jump',address:'127.0.0.1',port:22,username:'user',group:'未分组',system:'Linux'}
const b:HostProfile={...a,id:'b',name:'Target',jumpHostIds:['a']}
const rule:ForwardRule={id:'db',name:'DB',kind:'local',listenAddress:'127.0.0.1',listenPort:15432,targetAddress:'db.internal',targetPort:5432,enabled:true}
test('jump drafts reject missing references, self references and indirect cycles',()=>{
 expect(networkErrors(b,[a,b])).toEqual({})
 expect(networkErrors({...a,jumpHostIds:['b']},[a,b]).jumpHostIds).toBeTruthy()
 expect(networkErrors({...b,jumpHostIds:['missing']},[a,b]).jumpHostIds).toBeTruthy()
 expect(networkErrors({...b,jumpHostIds:['b']},[a,b]).jumpHostIds).toBeTruthy()
 expect(networkErrors({...b,timeoutSeconds:NaN},[a,b]).timeoutSeconds).toBeTruthy()
})
test('forward rules enforce listener namespaces and dynamic targets',()=>{
 expect(forwardError(rule)).toBe('')
 expect(forwardError({...rule,listenAddress:'0.0.0.999'})).toBeTruthy()
 expect(forwardError({...rule,targetPort:0})).toBeTruthy()
 expect(networkErrors({...b,forwards:[rule,{...rule,id:'socks',kind:'dynamic',targetAddress:'',targetPort:0}]},[a,b]).forwards).toBeTruthy()
 expect(networkErrors({...b,forwards:[rule,{...rule,id:'remote',kind:'remote'}]},[a,b]).forwards).toBeUndefined()
})
test('preview protects jump references and preserves session configuration snapshots',()=>{
 const snapshot:SavedWorkspace={revision:0,hosts:[a,b],groups:['未分组'],commands:[],commandGroups:['未分组']}
 expect(()=>previewChange(snapshot,{kind:'deleteHost',id:'a'})).toThrow()
 expect(()=>previewChange(snapshot,{kind:'saveHost',host:{...a,jumpHostIds:['b']}})).toThrow()
 const changed=previewChange(snapshot,{kind:'saveHost',host:{...b,forwards:[rule],timeoutSeconds:30}})
 expect(changed.hosts[1]?.forwards?.[0]?.targetAddress).toBe('db.internal')
 expect(snapshot.hosts[1]?.forwards).toBeUndefined()
})

test('proxy metadata validates endpoint and optional authentication without storing a password',()=>{
 expect(proxyError(emptyProxy())).toBe('')
 const proxy={kind:'socks5',address:'proxy.internal',port:1080,useAuthentication:true,username:'proxy-user'}
 expect(proxyError(proxy)).toBe('')
 expect(proxyError({...proxy,address:'http://proxy.internal'})).toBeTruthy()
 expect(proxyError({...proxy,kind:'http',username:'a:b'})).toBeTruthy()
 expect(proxyError({...proxy,username:'中'.repeat(86)})).toBeTruthy()
 expect(proxyError({...proxy,useAuthentication:false})).toBeTruthy()
 const snapshot:SavedWorkspace={revision:0,hosts:[a,b],groups:['未分组'],commands:[],commandGroups:['未分组']}
 const result=previewChange(snapshot,{kind:'saveHost',host:{...b,proxy}})
 expect(result.hosts[1]?.proxy?.address).toBe('proxy.internal')
 expect(result.hosts[1]?.jumpHostIds).toEqual(['a'])
 expect(result.hosts[1]?.proxy).not.toHaveProperty('password')
 expect(snapshot.hosts[1]?.proxy).toBeUndefined()
 expect(()=>previewChange(snapshot,{kind:'saveHost',host:{...b,proxy:{...proxy,port:0}}})).toThrow()
})
