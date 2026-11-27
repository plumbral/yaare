import {test,expect} from 'bun:test'
import { readdir } from 'node:fs/promises'
import {patchBatchHosts,toggleHostSelection,validateBatchHosts} from '../frontend/src/features/connections/batchHosts'
import {previewChange,type SavedWorkspace} from '../frontend/src/features/workspace/savedWorkspace'
import type {HostProfile} from '../frontend/src/features/workspace/mockGateway'
import {emptyProxy} from '../frontend/src/features/connections/networkConfig'
import paths from '../frontend/src/shared/icons/os/paths.json'
import vendors from '../frontend/src/shared/icons/cloudVendors.json'
import {osIconOptions,searchHostIcons} from '../frontend/src/shared/icons/OsIcon'

const host=(id:string):HostProfile=>({id,name:id,address:'127.0.0.1',port:22,username:'user',group:'未分组',system:'Linux',icon:'linux',authentication:'password',notes:'keep',timeoutSeconds:15,identityId:'existing',lastUsed:123,forwards:[],jumpHostIds:[]})
test('explicit batch fields preserve every unrelated setting and do not mutate source',()=>{
  const original=[host('a'),{...host('b'),port:2222,username:'other',notes:'different'}]
  const next=patchBatchHosts(original,{timeoutSeconds:45,proxy:{kind:'socks5',address:'proxy.test',port:1080,username:'',useAuthentication:false}})
  expect(next.map(h=>h.timeoutSeconds)).toEqual([45,45]);expect(next[1]!.port).toBe(2222)
  expect(next[1]!.notes).toBe('different');expect(next[1]!.identityId).toBe('existing')
  expect(original[0]!.proxy).toBeUndefined();expect(original[0]!.timeoutSeconds).toBe(15)
  expect(validateBatchHosts(next,original)).toBe('')
})
test('clear differs from keep and identity association copies public login metadata only',()=>{
  const original=[{...host('a'),proxy:{kind:'http',address:'proxy.test',port:8080,useAuthentication:false,username:''},jumpHostIds:['jump']}]
  const kept=patchBatchHosts(original,{})[0]!
  expect(kept.identityId).toBe('existing');expect(kept.notes).toBe('keep')
  const clear=patchBatchHosts(original,{identity:null,notes:'',jumpHostIds:[],proxy:emptyProxy(),group:''})[0]!
  expect(clear).toMatchObject({identityId:'',keyId:'',authentication:'password',useAgent:false,notes:'',jumpHostIds:[],group:'未分组',proxy:emptyProxy()})
  const identity={id:'new-id',name:'saved',username:'deploy',authentication:'key',keyId:'k',version:1,useAgent:true}
  const linked=patchBatchHosts(original,{identity})[0]!
  expect(linked).toMatchObject({identityId:'new-id',username:'deploy',authentication:'key',keyId:'',useAgent:false})
  expect(linked).not.toHaveProperty('password')
})
test('range selection uses all filtered IDs, including virtualized rows, and supports deselection',()=>{
  const ordered=Array.from({length:10000},(_,i)=>'host-'+i)
  const selected=toggleHostSelection(['host-2'],ordered,'host-9000',true,'host-2',true)
  expect(selected.length).toBe(8999);expect(selected[0]).toBe('host-2');expect(selected.at(-1)).toBe('host-9000')
  expect(toggleHostSelection(selected,ordered,'host-9000',false,'host-2',true)).toEqual([])
  expect(toggleHostSelection([],['b','a'],'a',true,'missing',true)).toEqual(['a'])
})
test('batch preview rejects dangling/cyclic jumps and preserves configuration on failure',()=>{
  const snapshot:SavedWorkspace={revision:7,hosts:[host('a'),host('b')],groups:['未分组'],commands:[],commandGroups:['未分组']}
  const cycle=patchBatchHosts([snapshot.hosts[0]!],{jumpHostIds:['a']})
  expect(validateBatchHosts(cycle,snapshot.hosts)).toContain('自身')
  expect(()=>previewChange(snapshot,{kind:'saveHosts',hosts:cycle})).toThrow()
  expect(()=>previewChange(snapshot,{kind:'saveHosts',hosts:[host('missing')]})).toThrow()
  expect(snapshot.revision).toBe(7);expect(snapshot.hosts[0]!.jumpHostIds).toEqual([])
  const next=previewChange(snapshot,{kind:'saveHosts',hosts:patchBatchHosts(snapshot.hosts,{notes:'same'})})
  expect(next.revision).toBe(8);expect(next.hosts.every(h=>h.notes==='same')).toBe(true)
  expect(snapshot.hosts[0]!.notes).toBe('keep')
})
test('local icon catalog has matching geometry and source assets',async()=>{
  expect(Object.keys(vendors)).toHaveLength(30)
  expect(osIconOptions.length).toBe(Object.keys(paths).length+Object.keys(vendors).length)
  expect(new Set(osIconOptions.map(i=>i.value)).size).toBe(osIconOptions.length)
  expect([...Object.keys(paths),...Object.keys(vendors)].sort()).toEqual(osIconOptions.map(i=>i.value).sort())
  for(const name of Object.keys(paths) as (keyof typeof paths)[]){
    const glyph=paths[name];expect(glyph.paths.length).toBeGreaterThan(0)
    expect(glyph.viewBox.split(/\s+/).map(Number).every(Number.isFinite)).toBe(true)
    expect(await Bun.file(new URL('../frontend/src/shared/icons/os/'+name+'.svg',import.meta.url)).exists()).toBe(true)
  }
  const notice = await Bun.file(new URL('../resources/licenses/cloud-provider-marks-NOTICE.txt', import.meta.url)).text()
  const assetDir = new URL('../frontend/src/shared/icons/cloud/', import.meta.url)
  const catalogAssets = new Set(Object.values(vendors).map(glyph => glyph.asset))
  expect((await readdir(assetDir)).sort()).toEqual([...catalogAssets].sort())
  for(const [id,glyph] of Object.entries(vendors)){
    const slug = id.slice('cloud-'.length)
    expect([slug+'.svg', 'generic-cloud.svg']).toContain(glyph.asset)
    const asset = Bun.file(new URL(glyph.asset, assetDir))
    expect(await asset.exists()).toBe(true)
    expect(asset.size).toBeGreaterThan(0)
    expect(notice).toContain(glyph.label+' ('+slug+')')
    expect(glyph.aliases.length).toBeGreaterThan(0)
  }
})

test('cloud icon search matches localized aliases and keeps alphabetical order',()=>{
  for(const query of ['aws','amazon','yama','亚马逊',' ＡＷＳ ']){
    expect(searchHostIcons(query).map(i=>i.value)).toContain('cloud-aws')
  }
  for(const [query,id] of [['阿里雲','cloud-alibaba'],['qcloud','cloud-tencent'],['甲骨文','cloud-oracle'],['google-cloud','cloud-google'],['debian','debian']]){
    expect(searchHostIcons(query!).map(i=>i.value)).toContain(id!)
  }
  expect(searchHostIcons('unknown fixture')).toEqual([])
  expect(searchHostIcons('aws', 'system')).toEqual([])
  expect(searchHostIcons('debian', 'cloud')).toEqual([])
  expect(searchHostIcons('亚马逊', 'cloud').map(option => option.value)).toContain('cloud-aws')
  expect(searchHostIcons('', 'system')).toHaveLength(Object.keys(paths).length)
  expect(searchHostIcons('', 'cloud')).toHaveLength(Object.keys(vendors).length)
  const labels=searchHostIcons('').map(i=>i.label)
  expect(labels).toEqual([...labels].sort(new Intl.Collator('en',{sensitivity:'base'}).compare))
})
