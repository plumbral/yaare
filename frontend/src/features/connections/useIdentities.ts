import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { useCallback, useEffect, useRef, useState } from 'react'
import { IdentityService, type Identity, type SaveIdentityRequest } from '../../shared/generated/mygo'
import { desktopAvailable } from '../../shared/bridge/application'
export type { Identity, SaveIdentityRequest }
const examples: Identity[] = [
 { id: 'demo-deploy', name: tr("生产部署"), username: 'deploy', authentication: 'key', keyId: '', version: 1 },
 { id: 'demo-admin', name: tr("测试管理员"), username: 'admin', authentication: 'password', keyId: '', version: 1 },
]
export function useIdentities(afterChange: () => Promise<boolean>) {
 const native = desktopAvailable()
 const [items,setItems]=useState<Identity[]>(native?[]:examples)
 const [error,setError]=useState(''); const [busy,setBusy]=useState(false);const lock=useRef(false)
 const reload=useCallback(async()=>{if(!native)return;try{setItems(await IdentityService.list());setError('')}catch{setError(tr("身份读取失败，请重试。"))}},[native])
 useEffect(()=>{void reload()},[reload])
 const save=async(request:SaveIdentityRequest)=>{
  if(lock.current)return false;lock.current=true;setBusy(true);setError('')
  try{if(native){await IdentityService.save(request);await afterChange();await reload()}
   else{const {id,name,username,authentication,keyId,useAgent,version}=request;setItems(old=>[...old.filter(i=>i.id!==id),{id,name,username,authentication,keyId,useAgent,version:version+1}])}
   return true
  }catch(cause){if(native)await reload();setError(cause instanceof Error?cause.message:tr("身份未保存，请重试。"));return false}
  finally{request.password='';request.privateKey='';request.passphrase='';lock.current=false;setBusy(false)}
 }
 const remove=async(i:Identity)=>{if(lock.current)return false;lock.current=true;setBusy(true);setError('');try{if(native){await IdentityService.remove(i.id,i.version);await afterChange();await reload()}else setItems(old=>old.filter(item=>item.id!==i.id));return true}catch(cause){setError(cause instanceof Error?cause.message:tr("身份未删除，请重试。"));return false}finally{lock.current=false;setBusy(false)}}
 return {items,error,busy,reload,save,remove,native}
}
