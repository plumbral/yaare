import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import type {Detection,Session,Offer,Sentry} from 'zmodem.js'
import type {TerminalTransport} from './remoteOutput'
import {terminalFileGateway,type TerminalFileScope,type TerminalReceiveView,type LocalFolder} from '../../shared/bridge/terminalFiles'

export type ZmodemView={open:boolean;mode:'send'|'receive'|'';phase:string;name:string;size:number;done:number;files:number;skipped:number;folder:string;existingSize:number;message:string;error:string}
const emptyView=():ZmodemView=>({open:false,mode:'',phase:'idle',name:'',size:0,done:0,files:0,skipped:0,folder:'',existingSize:0,message:'',error:''})
const signature=[42,42,24,66,48]
function zmodemStartAt(packet:Uint8Array,tail:Uint8Array):number|undefined{
 for(let i=0;i<packet.length;i++){if(packet[i]!==48)continue;let found=true;for(let n=1;n<5;n++){const index=i-n,value=index>=0?packet[index]:tail[tail.length+index];if(value!==signature[4-n]){found=false;break}}if(found)return i-4}
 return undefined
}
export function hasZmodemStart(packet:Uint8Array,tail:Uint8Array){return zmodemStartAt(packet,tail)!==undefined}
type Transport=TerminalTransport&Required<Pick<TerminalTransport,'beginTransfer'|'writeTransfer'|'endTransfer'>>
export class ZmodemEngine{
 private sentry?:Sentry;private session?:Session;private offer?:Offer;private scope?:TerminalFileScope;private folder:LocalFolder|null=null;private incoming:TerminalReceiveView|null=null
 private wire=Promise.resolve();private io=Promise.resolve();private text=Promise.resolve();private initializing=Promise.resolve();private tail=new Uint8Array(0);private dropPrefix=0;private generation=0;private disposed=false;private ending=false;private aborting=false;private probeLength=0;private probe:Uint8Array[]|null=null;private autoReceive=false;private unwritten=0;private queuedWire=0;private lastProgress=0
 view=emptyView()
 private unwatchPrompt?:()=>void
 private finishing=false;private awaitingPrompt=false
 constructor(private transport:Transport,private id:string,private attempt:number,private sink:(bytes:Uint8Array)=>Promise<void>,private changed:(view:ZmodemView)=>void,private gateway=terminalFileGateway){this.unwatchPrompt=transport.onRemotePrompt?.(()=>{if(this.active&&!this.ending){if(this.finishing&&!this.incoming)void this.complete();else void this.fail(Error(tr("服务器已返回命令行，原传输请求已结束。请重新运行 rz / sz。")),false)}})}
 private emit(patch:Partial<ZmodemView>,force=true){this.view={...this.view,...patch};const now=Date.now();if(force||now-this.lastProgress>=100){this.lastProgress=now;this.changed({...this.view})}}
 private terminal(bytes:Uint8Array){if(this.dropPrefix){const skip=Math.min(bytes.length,this.dropPrefix);bytes=bytes.subarray(skip);this.dropPrefix-=skip}if(!bytes.length)return;this.unwritten=0;if(bytes.length>=4)this.tail=bytes.slice(-4);else{const tail=new Uint8Array(this.tail.length+bytes.length);tail.set(this.tail);tail.set(bytes,this.tail.length);this.tail=tail.slice(-4)};this.text=this.text.then(()=>this.sink(bytes))}
 private sender(octets:number[]){if(this.session?.type==='receive'&&octets[0]===42&&octets[1]===42&&octets[2]===24&&octets[3]===66&&octets[4]===48&&octets[5]===56){this.finishing=true;this.emit({phase:'finishing',message:tr("文件已保存，正在确认本批结束…")})};const generation=this.generation,data=Uint8Array.from(octets);this.queuedWire+=data.length;if(this.queuedWire>262144){this.queuedWire-=data.length;data.fill(0);throw Error(tr("终端传输积压过多，已停止以保留文件。"))}
  this.wire=this.wire.then(async()=>{await Promise.resolve();await this.io;if(this.disposed||this.aborting||generation!==this.generation)return;await this.transport.writeTransfer(data)}).finally(()=>{this.queuedWire-=data.length;data.fill(0)})
  void this.wire.catch(cause=>this.fail(cause))
 }
 async consume(packet:Uint8Array){if(this.disposed)return;if(this.awaitingPrompt){this.terminal(packet);await this.text;return}
  try{
   if(!this.sentry){const start=zmodemStartAt(packet,this.tail);if(start===undefined){this.terminal(packet);await this.text;return};this.probeLength=packet.length-start;const Zmodem=(await import('zmodem.js')).default;if(this.disposed)return;const prefix=this.tail.slice();this.dropPrefix=prefix.length;this.probe=[]
    this.sentry=new Zmodem.Sentry({to_terminal:bytes=>{const chunk=Uint8Array.from(bytes);if(this.probe)this.probe.push(chunk);else this.terminal(chunk)},sender:bytes=>this.sender(bytes),on_detect:detection=>{this.initializing=this.detect(detection)},on_retract:()=>{if(!this.session&&this.view.phase==='initializing')void this.cancel(tr("服务器已取消传输请求。"))}})
    const combined=new Uint8Array(prefix.length+packet.length);combined.set(prefix);combined.set(packet,prefix.length);this.sentry.consume(combined)
   }else{if(!this.session&&!this.scope)this.probeLength+=packet.length;this.sentry.consume(packet)}
   this.unwritten+=packet.length;if(this.unwritten>2*1024*1024)throw Error(tr("服务器传输帧长时间未完成，接收已停止。"))
   await this.initializing;await Promise.resolve();await this.io;await this.wire;await this.text;if(!this.scope&&!this.session&&this.view.phase==='idle'&&this.probeLength>=21){for(const bytes of this.probe??[])this.terminal(bytes);this.probe=null;this.sentry=undefined;await this.text}
  }catch(cause){if(this.finishing&&typeof cause==='string'&&cause.startsWith('PROTOCOL: Only thing after ZFIN')){this.awaitingPrompt=true;this.terminal(Uint8Array.from(this.session?.get_pending_input()??[]));await this.wire;await this.text;return};await this.fail(cause)}
 }
 private async detect(detection:Detection){if(this.scope||this.ending){detection.deny();return};this.aborting=false;const generation=++this.generation;this.emit({...emptyView(),open:true,mode:detection.get_session_role(),phase:'initializing',message:tr("正在准备文件传输；会话录制将停止。")})
  try{const lease=await this.transport.beginTransfer();if(this.disposed||generation!==this.generation){await this.transport.endTransfer(true);return};this.scope={sessionId:this.id,attempt:this.attempt,lease};if(!detection.is_valid())throw Error(tr("服务器已取消初始化，请重新运行 rz / sz。"));this.session=detection.confirm();this.probe=null;this.dropPrefix=0;this.unwritten=0
   this.session.on('session_end',()=>{if(!this.ending){if(this.session?.aborted())void this.fail(Error(tr("服务器已取消传输，未完成的文件没有提交。")),false);else void this.complete()}})
   if(this.session.type==='receive'){this.session.on('offer',offer=>this.offered(offer));this.session.start();this.emit({phase:'waiting',message:tr("等待服务器提供文件…")})}else this.emit({phase:'choose',message:tr("请选择本次要上传的普通文件；远程 rz 决定文件覆盖策略。")})
  }catch(cause){await this.fail(cause)}
 }
 private offered(offer:Offer){if(this.ending||this.disposed)return;this.unwritten=0;const details=offer.get_details();if(!Number.isSafeInteger(details.size)||details.size<0||details.size>0xffffffff||this.view.files+this.view.skipped>=256){void this.fail(Error(tr("ZMODEM 每批最多 256 个文件，单文件必须小于 4 GiB。")));return}
  if(this.offer){void this.fail(Error(tr("服务器在当前文件处理完成前发送了另一个文件，请重新运行 sz。")));return}
  this.offer=offer;this.incoming=null;this.emit({mode:'receive',phase:'offer',name:details.name,size:details.size,done:0,message:tr("选择接收目录；已有同名文件时会再次确认。"),error:''});if(this.autoReceive&&this.folder)void this.receive()
 }
 async chooseFolder(){if(!this.scope)return;const generation=this.generation;try{const folder=await this.gateway.chooseFolder(this.scope);if(folder&&!this.disposed&&generation===this.generation){this.folder=folder;this.emit({folder:folder.name,error:''})}}catch(cause){if(generation===this.generation)this.emit({error:problem(cause)})}}
 async receive(policy?:'overwrite'|'keepBoth'){const offer=this.offer,scope=this.scope,folder=this.folder;if(!offer||!scope||!folder||this.ending||!['offer','conflict'].includes(this.view.phase))return;const generation=this.generation
  this.emit({phase:'preparing',error:'',message:tr("正在核对接收位置…")})
  try{const prepared=policy&&this.incoming?await this.gateway.resolve(this.incoming.id,policy):await this.gateway.prepare({scope,folderToken:folder.token,name:offer.get_details().name,size:offer.get_details().size});if(this.disposed||generation!==this.generation)return;this.incoming=prepared
   if(prepared.file.conflict){this.emit({phase:'conflict',existingSize:prepared.file.existingSize,message:tr("已有同名项目，请选择处理方式。")});return}
   this.autoReceive=true;let offset=0;const token=prepared.id;this.emit({phase:'transferring',name:prepared.file.name,message:tr("正在接收并写入本地文件…")})
   void offer.accept({on_input:octets=>{this.unwritten=0;const data=Uint8Array.from(octets),at=offset;offset+=data.length;this.io=this.io.then(async()=>{if(generation!==this.generation)throw Error(tr("传输已取消。"));await this.gateway.write(token,at,data);this.emit({done:at+data.length},false)}).finally(()=>data.fill(0));void this.io.catch(cause=>this.fail(cause))}}).then(()=>{if(generation!==this.generation)return;this.io=this.io.then(async()=>{await this.gateway.commit(token);if(generation===this.generation){this.incoming=null;this.offer=undefined;this.emit({files:this.view.files+1,done:this.view.size,phase:'waiting',message:tr("文件已保存，等待本批其余文件…")})}});void this.io.catch(cause=>this.fail(cause))},cause=>{if(generation===this.generation)void this.fail(cause)})
  }catch(cause){await this.fail(cause)}
 }
 async skip(){if(!this.offer||!this.scope||this.view.phase==='transferring')return;try{const offer=this.offer;if(this.incoming)await this.gateway.cancelReceive(this.incoming.id);this.incoming=null;this.offer=undefined;await offer.skip();this.emit({skipped:this.view.skipped+1});if(!this.offer){this.emit({phase:'waiting',message:tr("已跳过，等待服务器的下一个文件…"),error:''})}}catch(cause){await this.fail(cause)}}
 async upload(){if(!this.scope||this.session?.type!=='send'||this.view.phase==='transferring'||this.ending)return;const scope=this.scope,session=this.session,generation=this.generation;this.emit({phase:'choosing',error:''})
  try{const selection=await this.gateway.chooseUploads(scope);if(!selection){await this.cancel(tr("已取消文件选择。"));return};for(const file of selection.files){if(generation!==this.generation)return;this.emit({phase:'transferring',name:file.name,size:file.size,done:0,message:tr("正在上传文件…")});const transfer=await session.send_offer({name:file.name,size:file.size,mtime:new Date(file.modified)});if(!transfer){this.emit({skipped:this.view.skipped+1});continue};let done=0
    for(;;){if(generation!==this.generation)return;const chunk=await this.gateway.read(selection.id,file.name);try{done+=chunk.data.length;if(chunk.final){await transfer.end(Array.from(chunk.data));await this.wire;break};transfer.send(Array.from(chunk.data));await this.wire;this.emit({done},false)}finally{chunk.data.fill(0)}}this.unwritten=0;this.emit({files:this.view.files+1,done:file.size})
   }if(generation===this.generation)await session.close()
  }catch(cause){if(generation===this.generation)await this.fail(cause)}
 }
 private async complete(){if(this.ending||this.disposed)return;this.ending=true;try{await this.io;await this.wire;await this.cleanup(false);this.emit({phase:'completed',message:tr("{p0} 个文件完成{p1}。会话录制可手动重新开启。", this.view.files, (this.view.skipped?tr("，跳过 {p0} 个", this.view.skipped):'')),error:''})}catch(cause){this.emit({phase:'failed',error:problem(cause),message:tr("传输未确认完成，请检查文件。")});await this.cleanup(true)}finally{this.ending=false;this.aborting=false;this.resetProtocol()}}
 private async cleanup(abort:boolean){const scope=this.scope;this.scope=undefined;let error:unknown;try{if(scope)await this.gateway.cancel(scope)}catch(cause){error=cause}finally{await this.transport.endTransfer(abort)};if(error)throw error}
 async cancel(message=tr("已取消传输；已经完成的文件保留。"),abort=true){if(this.ending)return;this.ending=true;this.aborting=true;this.generation++;try{await this.io.catch(()=>{});await this.cleanup(abort);this.emit({phase:'cancelled',message,error:''})}catch(cause){this.emit({phase:'failed',message,error:problem(cause)})}finally{this.ending=false;this.resetProtocol()}}
 private async fail(cause:unknown,abort=true){if(this.ending||this.disposed)return;const message=problem(cause);await this.cancel(tr("传输未完成；原目标和已完成的文件保留。"),abort);this.emit({phase:'failed',error:message})}
 private resetProtocol(){this.sentry=undefined;this.session=undefined;this.offer=undefined;this.incoming=null;this.folder=null;this.autoReceive=false;this.unwritten=0;this.dropPrefix=0;this.probeLength=0;this.probe=null;this.finishing=false;this.awaitingPrompt=false;this.io=Promise.resolve();this.wire=Promise.resolve();this.initializing=Promise.resolve()}
 hide(){this.emit({open:false})}
 get active(){return!!this.scope||this.view.phase==='initializing'}
 async dispose(){if(this.disposed)return;this.unwatchPrompt?.();this.changed=()=>{};await this.cancel();this.disposed=true}
}
function problem(cause:unknown){return cause instanceof Error?cause.message:tr("终端文件传输未完成，请检查连接并重试。")}
