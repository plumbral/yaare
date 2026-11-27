export function watchElementFullscreen(element:HTMLElement,native:((enabled:boolean)=>Promise<boolean>)|undefined,changed:(enabled:boolean)=>void,failed:()=>void){
 const doc=element.ownerDocument
 let previous:boolean|undefined,queue=Promise.resolve()
 function sync(enabled:boolean){
  if(!native)return
  queue=queue.then(async()=>{
   if(enabled){if(previous===undefined)previous=await native!(true)}
   else if(previous!==undefined){const restore=previous;await native!(restore);previous=undefined}
  }).catch(()=>{failed();if(doc.fullscreenElement===element)void doc.exitFullscreen().catch(()=>{})})
 }
 const change=()=>{const enabled=doc.fullscreenElement===element;changed(enabled);sync(enabled)}
 doc.addEventListener('fullscreenchange',change)
 return ()=>{
  doc.removeEventListener('fullscreenchange',change)
  if(doc.fullscreenElement===element)void doc.exitFullscreen().catch(()=>{})
  sync(false)
 }
}
