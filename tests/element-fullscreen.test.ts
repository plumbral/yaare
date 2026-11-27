import {test,expect} from 'bun:test'
import {watchElementFullscreen} from '../frontend/src/features/workspace/elementFullscreen'

function fixture(){
 const doc=new EventTarget() as EventTarget&{fullscreenElement:unknown;exitFullscreen:()=>Promise<void>}
 doc.fullscreenElement=null
 const element={ownerDocument:doc} as unknown as HTMLElement
 doc.exitFullscreen=async()=>{doc.fullscreenElement=null;doc.dispatchEvent(new Event('fullscreenchange'))}
 return{doc,element,enter:()=>{doc.fullscreenElement=element;doc.dispatchEvent(new Event('fullscreenchange'))}}
}
const settle=()=>new Promise(resolve=>setTimeout(resolve,0))
test('element exit restores the native window, including an already fullscreen window',async()=>{
 for(const original of [false,true]){
  const f=fixture(),calls:boolean[]=[],states:boolean[]=[];let nativeState=original
  const dispose=watchElementFullscreen(f.element,async enabled=>{const before=nativeState;nativeState=enabled;calls.push(enabled);return before},state=>states.push(state),()=>{throw Error('unexpected failure')})
  f.enter();await settle();expect(nativeState).toBe(true)
  await f.doc.exitFullscreen();await settle();expect(nativeState).toBe(original)
  dispose();await settle();expect(calls).toEqual([true,original]);expect(states).toEqual([true,false])
 }
})
test('leaving a viewer while native enter is in flight still restores its prior placement state',async()=>{
 const f=fixture(),calls:boolean[]=[];let release!:(before:boolean)=>void
 const dispose=watchElementFullscreen(f.element,enabled=>{calls.push(enabled);return enabled?new Promise<boolean>(resolve=>{release=resolve}):Promise.resolve(true)},()=>{},()=>{})
 f.enter();await settle();dispose();release(false);await settle()
 expect(calls).toEqual([true,false]);expect(f.doc.fullscreenElement).toBeNull()
 f.enter();await settle();expect(calls).toEqual([true,false])
})
test('browser element fullscreen never requires a native window bridge',async()=>{
 const f=fixture(),states:boolean[]=[]
 const dispose=watchElementFullscreen(f.element,undefined,enabled=>states.push(enabled),()=>{})
 f.enter();await f.doc.exitFullscreen();await settle();dispose();expect(states).toEqual([true,false])
})
