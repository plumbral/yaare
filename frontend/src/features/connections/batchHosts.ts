import type { Identity } from './useIdentities'
import type { HostProfile } from '../workspace/mockGateway'
import { validateHost } from '../workspace/mockGateway'
import type { OsIconName } from '../../shared/icons/OsIcon'
import type { ProxyConfig } from '../../shared/generated/mygo'
import { networkErrors } from './networkConfig'

export interface BatchHostPatch {
  group?:string;icon?:OsIconName;identity?:Identity|null;proxy?:ProxyConfig
  jumpHostIds?:string[];timeoutSeconds?:number;notes?:string;pinned?:boolean
}
export function patchBatchHosts(hosts:readonly HostProfile[],patch:BatchHostPatch):HostProfile[] {
  return hosts.map(host=>{
    const next={...host}
    if(patch.group!==undefined)next.group=patch.group||'未分组'
    if(patch.icon!==undefined)next.icon=patch.icon
    if(patch.identity!==undefined){
      next.identityId=patch.identity?.id??'';next.keyId='';next.useAgent=false
      next.authentication=patch.identity?.authentication==='key'?'key':'password'
      if(patch.identity)next.username=patch.identity.username
    }
    if(patch.proxy!==undefined)next.proxy={...patch.proxy}
    if(patch.jumpHostIds!==undefined)next.jumpHostIds=[...patch.jumpHostIds]
    if(patch.timeoutSeconds!==undefined)next.timeoutSeconds=patch.timeoutSeconds
    if(patch.notes!==undefined)next.notes=patch.notes
    if(patch.pinned!==undefined)next.pinned=patch.pinned
    return next
  })
}
export function validateBatchHosts(incoming:HostProfile[],all:readonly HostProfile[]):string {
  const replacements=new Map(incoming.map(h=>[h.id,h]))
  const combined=all.map(h=>replacements.get(h.id)??h)
  for(const host of incoming){
    const errors={...validateHost(host),...networkErrors(host,combined)}
    const message=Object.values(errors).find(Boolean)
    if(message)return `${host.name}：${message}`
  }
  return ''
}
export function toggleHostSelection(selected:readonly string[],ordered:readonly string[],id:string,checked:boolean,anchor:string,range:boolean):string[] {
  const start=ordered.indexOf(anchor),end=ordered.indexOf(id)
  const ids=range&&start>=0&&end>=0?ordered.slice(Math.min(start,end),Math.max(start,end)+1):[id]
  const result=new Set(selected)
  for(const current of ids)if(checked)result.add(current);else result.delete(current)
  return ordered.filter(current=>result.has(current))
}
