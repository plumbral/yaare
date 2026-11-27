import {expect,test} from 'bun:test'
import {rowMotion} from '../frontend/src/shared/ui/listMotion'

test('list motion reads CSS seconds or milliseconds and respects reduced motion',()=>{
 const originalMedia=globalThis.matchMedia,originalStyle=globalThis.getComputedStyle
 let reduced=false,time='.165s',calls:KeyframeAnimationOptions[]=[]
 Object.assign(globalThis,{
  matchMedia:()=>({matches:reduced}),
  getComputedStyle:()=>({getPropertyValue:(name:string)=>name==='--duration-fast-max'?time:'ease-out'}),
 })
 const element={animate:(_frames:Keyframe[],options:KeyframeAnimationOptions)=>{calls.push(options);return {} as Animation}} as HTMLElement
 try{
  rowMotion(element,48);expect(calls.at(-1)?.duration).toBe(165)
  time='165ms';rowMotion(element,-48);expect(calls.at(-1)?.duration).toBe(165)
  reduced=true;rowMotion(element,1000,true);expect(calls).toHaveLength(2)
 }finally{Object.assign(globalThis,{matchMedia:originalMedia,getComputedStyle:originalStyle})}
})
