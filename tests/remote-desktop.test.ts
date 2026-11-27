import {test,expect} from 'bun:test'
import {createVNCChannel} from '../frontend/src/features/workspace/vncChannel'
test('VNC raw channels preserve early binary packets and copy reused send buffers before awaiting IPC',async()=>{
 const sent:string[]=[],received:number[]=[],errors:Error[]=[];let closed=0
 const transport=createVNCChannel(async(_id,data)=>{sent.push(data)},()=>closed++,e=>errors.push(e))
 transport.packet({id:'vnc',type:'ready',data:''});transport.packet({id:'vnc',type:'data',data:'AP+A'})
 transport.channel.onmessage=event=>received.push(...new Uint8Array(event.data));expect(received).toEqual([])
 await Promise.resolve();expect(received).toEqual([0,255,128])
 transport.packet({id:'other',type:'data',data:'AQ=='})
 const bytes=new Uint8Array([0,255,128]);transport.channel.send(bytes);bytes.fill(9)
 await new Promise(resolve=>setTimeout(resolve,0));expect(sent).toEqual(['AP+A']);expect(received).toEqual([0,255,128])
 transport.end();transport.end();expect(closed).toBe(1);expect(errors).toEqual([])
})
test('VNC send failure closes only its raw channel without retry',async()=>{
 let writes=0,closed=0;const errors:Error[]=[]
 const transport=createVNCChannel(async()=>{writes++;throw Error('write failed')},()=>closed++,e=>errors.push(e))
 transport.packet({id:'vnc',type:'ready',data:''});transport.channel.send(new Uint8Array([1]));transport.channel.send(new Uint8Array([2]))
 await new Promise(resolve=>setTimeout(resolve,0));expect(writes).toBe(1);expect(closed).toBe(1);expect(errors).toHaveLength(1)
})
