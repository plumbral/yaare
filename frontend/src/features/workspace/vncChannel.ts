import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import type {DesktopPacket} from '../../shared/bridge/desktop'
import {decodeBase64,encodeBase64} from '../../shared/bridge/base64'

export function createVNCChannel(write:(id:string,data:string)=>Promise<void>,stop:()=>void,onError:(error:Error)=>void){
 let id='',pending=0,wire=Promise.resolve(),ended=false
 let receive:((event:{data:ArrayBuffer})=>void)|null=null,early:ArrayBuffer[]=[],earlyBytes=0
 const channel={
  readyState:0,binaryType:'arraybuffer',protocol:'',
  onopen:null as null|(()=>void),onclose:null as null|((event:{code:number;reason:string;wasClean:boolean})=>void),
  get onmessage(){return receive},set onmessage(handler:typeof receive){receive=handler;queueMicrotask(()=>{if(ended||!receive)return;const packets=early;early=[];earlyBytes=0;for(const data of packets){if(ended)return;receive?.({data})}})},
  onerror:null as null|((event:Event)=>void),
  close:()=>{if(ended)return;ended=true;channel.readyState=3;early=[];earlyBytes=0;stop();channel.onclose?.({code:1000,reason:'',wasClean:true})},
  send:(value:ArrayBuffer|ArrayBufferView)=>{if(ended||channel.readyState!==1)return;const bytes=value instanceof ArrayBuffer?new Uint8Array(value):new Uint8Array(value.buffer,value.byteOffset,value.byteLength)
   if(bytes.byteLength>64*1024||pending+bytes.byteLength>1024*1024){fail(new Error(tr("VNC 输入等待过多，连接已停止；不会自动重发。")));return}
   const data=encodeBase64(bytes),size=bytes.byteLength;pending+=size;wire=wire.then(async()=>{if(!ended)await write(id,data)}).catch(error=>fail(error instanceof Error?error:new Error(tr("VNC 输入未确认。")))).finally(()=>{pending-=size})
  },
 }
 function fail(error:Error){if(ended)return;onError(error);channel.onerror?.(new Event('error'));channel.close()}
 return {channel,packet(packet:DesktopPacket){if(ended)return;if(packet.type==='ready'){id=packet.id;channel.readyState=1;channel.onopen?.()}else if(packet.type==='data'&&packet.id===id){const bytes=decodeBase64(packet.data),data=bytes.buffer.slice(bytes.byteOffset,bytes.byteOffset+bytes.byteLength) as ArrayBuffer;if(!receive||early.length){earlyBytes+=data.byteLength;if(earlyBytes>1024*1024){fail(new Error(tr("VNC 初始化等待过多，请重新连接。")));return};early.push(data)}else receive({data})}},end(){channel.close()},fail}
}
