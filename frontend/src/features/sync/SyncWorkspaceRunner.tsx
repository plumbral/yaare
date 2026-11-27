import {useEffect,useRef} from 'react'
import {desktopAvailable} from '../../shared/bridge/application'
import {cloudStatus,followCloudStatus,openCloudWatch} from '../../shared/bridge/cloud'

export default function SyncWorkspaceRunner({onReload}:{onReload:()=>Promise<void>}){
 const reload=useRef(onReload)
 reload.current=onReload
 useEffect(()=>{
  if(!desktopAvailable())return
  return followCloudStatus(cloudStatus,openCloudWatch,()=>reload.current())
 },[])
 return null
}
