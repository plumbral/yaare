import {Channel} from 'mygo-runtime'
import {DesktopService,type DesktopPacket,type DesktopTarget,type X11Draft} from '../generated/mygo'
import {desktopAvailable} from './application'
export type {DesktopTarget,DesktopPacket} from '../generated/mygo'
export const desktopGateway={
 available:desktopAvailable,
 x11(session:string,attempt:number,draft:X11Draft,onState:(kind:string)=>void,onError:(cause:unknown)=>void,onDone:()=>void){
  const channel=new Channel<DesktopPacket>();let closed=false
  void DesktopService.x11(session,attempt,draft,channel).catch(e=>{if(!closed)onError(e)}).finally(()=>{if(!closed)onDone()})
  draft.cookie=''
  void(async()=>{try{for await(const packet of channel){if(!closed)onState(packet.type)}}catch(e){if(!closed)onError(e)}})()
  return()=>{closed=true;channel.close()}
 },
 vnc(session:string,attempt:number,target:DesktopTarget,onPacket:(packet:DesktopPacket)=>void,onError:(cause:unknown)=>void,onDone:()=>void){
  const channel=new Channel<DesktopPacket>();let closed=false
  void DesktopService.vnc(session,attempt,target,channel).catch(e=>{if(!closed)onError(e)}).finally(()=>{if(!closed)onDone()})
  void(async()=>{try{for await(const packet of channel){if(!closed)onPacket(packet)}}catch(e){if(!closed)onError(e)}})()
  return()=>{closed=true;channel.close()}
 },
 rdp(session:string,attempt:number,target:DesktopTarget,user:string,onReady:()=>void,onError:(cause:unknown)=>void,onDone:()=>void){
  const channel=new Channel<DesktopPacket>();let closed=false
  void DesktopService.rdp(session,attempt,target,user,channel).catch(e=>{if(!closed)onError(e)}).finally(()=>{if(!closed)onDone()})
  void(async()=>{try{for await(const packet of channel){if(!closed&&packet.type==='ready')onReady()}}catch(e){if(!closed)onError(e)}})()
  return()=>{closed=true;channel.close()}
 },
 write:DesktopService.write,
}
