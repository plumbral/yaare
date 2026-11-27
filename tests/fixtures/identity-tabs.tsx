import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import '@astryxdesign/core/reset.css'
import '@astryxdesign/core/astryx.css'
import { Theme } from '@astryxdesign/core/theme'
import { Stack } from '@astryxdesign/core/Stack'
import { workbenchTheme } from '../../frontend/src/shared/theme/currentTheme'
import IdentityPanel from '../../frontend/src/features/connections/IdentityPanel'
import type { Identity, useIdentities } from '../../frontend/src/features/connections/useIdentities'
import type { KeyReference } from '../../frontend/src/shared/bridge/keys'

const identities:Identity[]=Array.from({length:80},(_,index)=>({id:`identity-${index}`,version:1,name:`部署 ${String(index).padStart(2,'0')}`,username:`user-${index}`,authentication:'key',keyId:index===0?'key-0':''}))
const examples:KeyReference[]=Array.from({length:80},(_,index)=>({id:`key-${index}`,name:`fixture-key-${String(index).padStart(3,'0')}`,format:'OPENSSH PRIVATE KEY',available:index!==1,source:index===79?'stored':'file'}))
function Fixture(){
 const [keys,setKeys]=useState(examples);const [error,setError]=useState('');const [attempt,setAttempt]=useState(0)
 const store:ReturnType<typeof useIdentities>={items:identities,error:'',busy:false,native:true,reload:async()=>{},save:async()=>false,remove:async()=>false}
 return <Theme theme={workbenchTheme} mode="light"><Stack height="100dvh"><IdentityPanel active store={store} hosts={[]} keys={keys} keyBusy={false} keyError={error}
  keyUsage={id=>id==='key-2'?1:0} onKeyAdded={async key=>setKeys(old=>[...old,key])} onChooseKey={async()=>null}
  onForgetKey={async id=>{setAttempt(value=>value+1);if(!attempt){setError('模拟移除失败');return false}setError('');setKeys(old=>old.filter(key=>key.id!==id));return true}}/>
 </Stack></Theme>
}
createRoot(document.getElementById('app')!).render(<Fixture/> )
Object.assign(window,{async runIdentityTabsRegression(){
 const wait=()=>new Promise(resolve=>setTimeout(resolve,40))
 const click=(id:string)=>document.getElementById(id)!.click()
 const input=(id:string,text:string)=>{const element=document.getElementById(id) as HTMLInputElement;Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value')!.set!.call(element,text);element.dispatchEvent(new Event('input',{bubbles:true}))}
 const panel=(id:string)=>document.getElementById(id)!
 const button=(label:string)=>document.querySelector<HTMLButtonElement>(`button[aria-label="${label}"]`)!
 const disabled=(element:HTMLButtonElement)=>element.disabled||element.getAttribute('aria-disabled')==='true'
 const result:Record<string,boolean>={}
 await wait()
 result.defaultsToIdentity=panel('identity-mode-identities').getAttribute('aria-selected')==='true'&&panel('identity-key-panel').hidden
 const identityPanel=panel('identity-list-panel');identityPanel.scrollTop=360;await wait()
 click('identity-mode-keys');await wait()
 result.keyActionsShown=!!panel('identity-key-add')&&!document.getElementById('identity-add')
 result.hiddenIdentityPanel=identityPanel.hidden&&getComputedStyle(identityPanel).display==='none'
 const keysPanel=panel('identity-key-panel');keysPanel.scrollTop=520;await wait()
 click('identity-mode-identities');await wait()
 result.identityScrollRestored=Math.abs(identityPanel.scrollTop-360)<1
 input('identity-search','部署 01');await wait()
 click('identity-mode-keys');await wait()
 result.keyScrollRestored=Math.abs(keysPanel.scrollTop-520)<1
 input('identity-key-search','fixture-key-00');await wait()
 result.referencedIdentityProtected=disabled(button('移除密钥引用 fixture-key-000'))
 result.activeSessionProtected=disabled(button('移除密钥引用 fixture-key-002'))
 button('移除密钥引用 fixture-key-000').click();button('移除密钥引用 fixture-key-002').click();await wait()
 result.protectedRemovalDoesNotRun=!!button('移除密钥引用 fixture-key-000')&&!!button('移除密钥引用 fixture-key-002')&&!keysPanel.querySelector('[role="alert"]')
 result.unavailableKeyShown=keysPanel.textContent!.includes('文件不可用')
 click('identity-mode-identities');await wait()
 result.identitySearchRetained=(panel('identity-search') as HTMLInputElement).value==='部署 01'&&identityPanel.querySelectorAll('li').length===1
 click('identity-mode-keys');await wait()
 result.keySearchRetained=(panel('identity-key-search') as HTMLInputElement).value==='fixture-key-00'
 button('移除密钥引用 fixture-key-003').click();await wait()
 result.failedRemovalKeepsKey=!!button('移除密钥引用 fixture-key-003')&&!!keysPanel.querySelector('[role="alert"]')
 button('移除密钥引用 fixture-key-003').click();await wait()
 result.successfulRemovalFeedback=!button('移除密钥引用 fixture-key-003')&&document.body.textContent!.includes('原文件未删除')
 result.removeReturnsFocus=document.activeElement?.id==='identity-key-search'
 input('identity-key-search','fixture-key-079');await wait();button('删除密钥 fixture-key-079').click();await wait()
 result.storedDeleteRequiresConfirmation=!!document.querySelector('dialog[open]')&&!!button('删除密钥 fixture-key-079')
 ;[...document.querySelectorAll<HTMLButtonElement>('dialog[open] button')].find(element=>element.textContent==='取消')!.click();await wait()
 result.cancelPreservesStoredKey=!!button('删除密钥 fixture-key-079')&&!document.querySelector('dialog[open]')
 button('删除密钥 fixture-key-079').click();await wait()
 ;[...document.querySelectorAll<HTMLButtonElement>('dialog[open] button')].find(element=>element.textContent==='确认删除')!.click();await wait()
 result.storedDeleteCommitsAfterConfirmation=!button('删除密钥 fixture-key-079')&&!document.querySelector('dialog[open]')
 click('identity-key-add');await wait()
 result.addKeyOpensDialog=!!document.querySelector('dialog[open] form[aria-label="添加密钥"]')
 return result
}})
