import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import '@astryxdesign/core/reset.css'
import '@astryxdesign/core/astryx.css'
import { Theme } from '@astryxdesign/core/theme'
import { Button } from '@astryxdesign/core/Button'
import KeyEditor from '../../frontend/src/features/connections/KeyEditor'
import { workbenchTheme } from '../../frontend/src/shared/theme/currentTheme'
import type { SaveKeyRequest,KeyReference } from '../../frontend/src/shared/bridge/keys'

let picks=0,saves=0,discards=0,added=0,receivedText=false,lastRequest:SaveKeyRequest|undefined
let finish:(ok:boolean)=>void=()=>{}
const gateway={available:()=>true,pick:async()=>{picks++;return {token:'fixture-selection',name:'fixture-file',format:'OpenSSH'}},discard:async()=>{discards++},save:(request:SaveKeyRequest)=>{
 saves++;lastRequest=request;receivedText=request.privateKey==='fixture-only-not-a-key'&&request.passphrase==='fixture-only-phrase'
 return new Promise<KeyReference>((resolve,reject)=>{finish=ok=>ok?resolve({id:'saved',name:request.name,format:'OpenSSH',available:true,source:request.source==='text'?'stored':'file'}):reject(new Error('模拟保存失败'))})
}}
function Fixture(){const [open,setOpen]=useState(true),[revision,setRevision]=useState(0);return <Theme theme={workbenchTheme} mode="light"><Button label="再次添加" onClick={()=>{setRevision(value=>value+1);setOpen(true)}}/><KeyEditor key={revision} isOpen={open} onClose={()=>setOpen(false)} onSaved={async()=>{added++}} gateway={gateway}/></Theme>}
createRoot(document.getElementById('app')!).render(<Fixture/> )
Object.assign(window,{async runKeyEditorRegression(){
 const wait=()=>new Promise(resolve=>setTimeout(resolve,40))
 const button=(name:string)=>[...document.querySelectorAll<HTMLButtonElement>('dialog[open] button')].find(item=>item.getAttribute('aria-label')===name||item.textContent===name)!
 const radio=(value:string)=>document.querySelector<HTMLButtonElement>(`dialog[open] [role="radio"][data-value="${value}"]`)!
 const input=(name:string,text:string)=>{const element=document.querySelector<HTMLInputElement|HTMLTextAreaElement>(`dialog[open] [name="${name}"]`)!;const prototype=element instanceof HTMLTextAreaElement?HTMLTextAreaElement.prototype:HTMLInputElement.prototype;Object.getOwnPropertyDescriptor(prototype,'value')!.set!.call(element,text);element.dispatchEvent(new Event('input',{bubbles:true}));return element}
 const form=()=>document.querySelector<HTMLFormElement>('dialog[open] form')!
 const result:Record<string,boolean>={};await wait()
 result.opensBeforeFilePicker=picks===0&&saves===0&&!!form()
 button('选择私钥文件').click();await wait()
 result.fileSelectionNotCommitted=picks===1&&saves===0
 radio('text').click();await wait();result.switchDiscardsSelection=discards===1
 input('key-name','测试草稿');const privateInput=input('saved-key-content','fixture-only-not-a-key'),secret=input('saved-key-passphrase','fixture-only-phrase');await wait()
 form().requestSubmit();await wait()
 result.secretSubmittedWriteOnly=receivedText
 result.secretClearedOnSubmit=privateInput.value===''&&secret.value===''
 form().requestSubmit();await wait();result.noDuplicateSubmit=saves===1
 finish(false);await wait()
 result.failureKeepsName=document.querySelector<HTMLInputElement>('dialog[open] [name="key-name"]')?.value==='测试草稿'&&!!document.querySelector('dialog[open] [role="alert"]')
 result.requestClearedAfterFailure=lastRequest?.privateKey===''&&lastRequest?.passphrase===''
 input('saved-key-content','fixture-only-not-a-key');input('saved-key-passphrase','fixture-only-phrase');await wait()
 radio('file').click();await wait();radio('text').click();await wait()
 result.sourceChangeClearsSecrets=document.querySelector<HTMLTextAreaElement>('dialog[open] textarea')?.value===''&&document.querySelector<HTMLInputElement>('dialog[open] [name="saved-key-passphrase"]')?.value===''
 button('取消').click();await wait()
 result.cancelDoesNotSave=saves===1&&document.querySelectorAll('dialog[open]').length===0
 ;[...document.querySelectorAll<HTMLButtonElement>('button')].find(element=>element.textContent==='再次添加'||element.getAttribute('aria-label')==='再次添加')!.click();await wait()
 result.reopenClearsDraft=document.querySelector<HTMLInputElement>('dialog[open] [name="key-name"]')?.value===''
 radio('text').click();await wait();input('key-name','成功保存');input('saved-key-content','fixture-only-not-a-key');input('saved-key-passphrase','fixture-only-phrase');await wait();form().requestSubmit();await wait();finish(true);await wait()
 result.saveClosesAfterCommit=added===1&&document.querySelectorAll('dialog[open]').length===0
 return result
}})
