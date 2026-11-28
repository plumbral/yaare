import {expect,test} from 'bun:test'
import {spawn,spawnSync} from 'node:child_process'
import {mkdtemp,writeFile,readFile,open,rename,unlink,rm,mkdir} from 'node:fs/promises'
import {join,resolve} from 'node:path'
import {tmpdir} from 'node:os'
import {randomUUID} from 'node:crypto'
import {ZmodemEngine,hasZmodemStart,type ZmodemView} from '../frontend/src/features/terminal/zmodemEngine'
import {terminalFileGateway} from '../frontend/src/shared/bridge/terminalFiles'
import {decodeBase64,encodeBase64} from '../frontend/src/shared/bridge/base64'
import type {FileHandle} from 'node:fs/promises'

test('terminal binary codec and lazy ZMODEM signature survive every chunk boundary',()=>{
 for(const size of [0,1,8192,32768,65536]){const data=Uint8Array.from({length:size},(_,i)=>i%256);expect(decodeBase64(encodeBase64(data))).toEqual(data)}
 const header=Uint8Array.from([42,42,24,66,48]);for(let split=1;split<=5;split++)expect(hasZmodemStart(header.subarray(split),header.subarray(0,split).slice(-4))).toBe(split<5)
 expect(hasZmodemStart(new TextEncoder().encode('normal 0 terminal output'),new Uint8Array())).toBe(false)
})

test('a false signature stays byte exact and cancellation permits a fresh transfer on the same pane',async()=>{
 const header=Uint8Array.from(require('zmodem.js/src/zheader').Header.build('ZRQINIT').to_hex())
 const plain=new TextEncoder().encode('normal output **\x18B0 invalid header followed by ordinary text\n'),output:number[]=[],writes:Uint8Array[]=[]
 let begins=0,ends=0
 const engine=new ZmodemEngine({read:async function*(){},send:()=>false,resize:()=>{},beginTransfer:async()=>{begins++;return'lease-'+begins},writeTransfer:async data=>{writes.push(data.slice())},endTransfer:async()=>{ends++}},'test',1,async data=>{output.push(...data)},()=>{})
 await engine.consume(plain);expect(Uint8Array.from(output)).toEqual(plain);expect(begins).toBe(0)
 for(const byte of header)await engine.consume(Uint8Array.of(byte))
 expect(engine.active).toBe(true);expect(begins).toBe(1);expect(writes.length).toBeGreaterThan(0)
 await engine.cancel();expect(engine.active).toBe(false);expect(ends).toBe(1)
 const firstWrites=writes.length;await engine.consume(new TextEncoder().encode('shell resumed\n'));await engine.consume(header)
 expect(begins).toBe(2);expect(engine.active).toBe(true);expect(writes.length).toBeGreaterThan(firstWrites)
 await engine.dispose();expect(ends).toBe(2)
})

test('a server prompt expires a pending transfer without sending protocol cancellation into the shell',async()=>{
 let prompt:(()=>void)|undefined,cleaned=0,unwatched=0;const endings:boolean[]=[]
 const gateway={...terminalFileGateway,cancel:async()=>{cleaned++}}
 const engine=new ZmodemEngine({read:async function*(){},send:()=>false,resize:()=>{},beginTransfer:async()=>'lease',writeTransfer:async()=>{},endTransfer:async abort=>{endings.push(abort)},onRemotePrompt:callback=>{prompt=callback;return()=>{unwatched++}}},'test',1,async()=>{},()=>{},gateway)
 await engine.consume(Uint8Array.from(require('zmodem.js/src/zheader').Header.build('ZRQINIT').to_hex()))
 expect(engine.active).toBe(true);prompt!();await new Promise(r=>setTimeout(r,0))
 expect(engine.active).toBe(false);expect(engine.view.phase).toBe('failed');expect(engine.view.error).toContain('服务器已返回命令行');expect(endings).toEqual([false]);expect(cleaned).toBe(1)
 await engine.dispose();expect(unwatched).toBe(1)
})

test('peer abort is not completion; a closing ZFIN may finish only after a verified shell prompt',async()=>{
 const header=require('zmodem.js/src/zheader').Header
 for(const abort of [true,false]){let prompt:(()=>void)|undefined;const endings:boolean[]=[]
  const engine=new ZmodemEngine({read:async function*(){},send:()=>false,resize:()=>{},beginTransfer:async()=>'lease',writeTransfer:async()=>{},endTransfer:async aborted=>{endings.push(aborted)},onRemotePrompt:callback=>{prompt=callback;return()=>{}}},'test',1,async()=>{},()=>{},{...terminalFileGateway,cancel:async()=>{}})
  await engine.consume(Uint8Array.from(header.build('ZRQINIT').to_hex()))
  if(abort){await engine.consume(Uint8Array.from([24,24,24,24,24,24,24,24]));await new Promise(r=>setTimeout(r,0));expect(engine.view.phase).toBe('failed');expect(engine.view.error).toContain('取消传输')}
  else{await engine.consume(Uint8Array.from(header.build('ZFIN').to_hex()));await engine.consume(new TextEncoder().encode('remote prompt$ '));expect(engine.view.phase).not.toBe('completed');prompt!();await new Promise(r=>setTimeout(r,0));expect(engine.view.phase).toBe('completed');expect(engine.view.files).toBe(0)}
  expect(endings[0]).toBe(false);await engine.dispose()
 }
})

const docker='docker',container='yaare-test-openssh'
test('a missing final OO releases a native receive and preserves trailing shell output', async () => {
  const header = require('zmodem.js/src/zheader').Header
  const init = Uint8Array.from(header.build('ZRQINIT').to_hex())
  await Promise.all(['','remote prompt$ '].map(async trailing => {
    const endings: boolean[] = []
    const output: number[] = []
    let settled!: () => void
    const unconfirmed = new Promise<void>(resolve => { settled = resolve })
    const engine = new ZmodemEngine({
      read: async function*(){}, send: () => false, resize: () => {}, beginTransfer: async () => 'lease',
      writeTransfer: async () => {}, endTransfer: async abort => { endings.push(abort) },
    }, 'test', 1, async chunk => { output.push(...chunk) }, view => {
      if (view.phase === 'unconfirmed') settled()
    }, { ...terminalFileGateway, cancel: async () => {} })
    try {
      await engine.consume(init)
      output.length = 0
      await engine.consume(Uint8Array.from(header.build('ZFIN').to_hex()))
      if (trailing) await engine.consume(new TextEncoder().encode(trailing))
      await unconfirmed
      expect(engine.active).toBe(false)
      expect(engine.view.files).toBe(0)
      expect(endings).toEqual([false])
      expect(new TextDecoder().decode(Uint8Array.from(output))).toBe(trailing)
      await engine.consume(init)
      expect(engine.active).toBe(true)
      await engine.consume(Uint8Array.from([...header.build('ZFIN').to_hex(),79,79]))
      await Bun.sleep(0)
      expect(engine.view.phase).toBe('completed')
    } finally { await engine.dispose() }
  }))
}, 6500)

const realTest=process.env.YAARE_ZMODEM_TEST==='1'?test:test.skip
test('ZFIN, OO and trailing terminal text finish correctly across coalesced or split frames',async()=>{
 const Zmodem=(await import('zmodem.js')).default,header=require('zmodem.js/src/zheader').Header
 for(const split of [0,1,2]){let completed=0,output:number[]=[];const sentry=new Zmodem.Sentry({sender:()=>{},on_retract:()=>{},to_terminal:bytes=>output.push(...bytes),on_detect:detection=>{const session=detection.confirm();session.on('session_end',()=>{completed++});session.start()}})
  sentry.consume(header.build('ZRQINIT').to_hex());output=[];const fin=header.build('ZFIN').to_hex(),tail=Array.from(new TextEncoder().encode('remote prompt$ '))
  if(split===0)sentry.consume([...fin,79,79,...tail]);else if(split===1){sentry.consume([...fin,79]);sentry.consume([79,...tail])}else{sentry.consume(fin);sentry.consume([79,79,...tail])}
  expect(completed).toBe(1);expect(new TextDecoder().decode(Uint8Array.from(output))).toBe('remote prompt$ ')
 }
})
function command(args:string[]){const result=spawnSync(docker,args,{encoding:'utf8'});if(result.status!==0)throw Error('isolated Docker fixture command failed: '+result.stderr.slice(0,200));return result.stdout}

async function fixture(mode:'receive'|'send',failCommit=false,pty=false,options:{bytes?:number;delay?:number;shell?:'dash';noSFTP?:boolean}={}){
 const root=await mkdtemp(join(tmpdir(),'yaare-zmodem-')),remote='/home/yaaretest/files/yaare-zmodem-'+randomUUID()
 const source=join(root,'selected.bin'),target=join(root,'received.bin'),data=Uint8Array.from({length:options.bytes??1024*1024},(_,i)=>i%256)
 await writeFile(source,data);command(['exec','--user','yaaretest',container,'mkdir','-p',remote])
 if(mode==='receive'){command(['cp',source,container+':'+remote+'/received.bin']);command(['exec','--user','root',container,'chown','yaaretest:yaaretest',remote+'/received.bin'])}
 let file:FileHandle|undefined,reader:FileHandle|undefined,offset=0,commits=0,maximumChunk=0,readOffset=0,view:ZmodemView|undefined,accepting=false
 const stage=join(root,'incoming.part');const original=Buffer.from('original local target');if(failCommit)await writeFile(target,original)
 const gateway:typeof terminalFileGateway={...terminalFileGateway,chooseFolder:async()=>({token:'private-root',name:'fixture'}),prepare:async request=>{file=await open(stage,'wx');return{id:'incoming',file:{name:request.name,size:request.size,conflict:false,existingSize:0}}},resolve:async()=>{throw Error('unexpected conflict')},write:async(_id,at,chunk)=>{expect(at).toBe(offset);maximumChunk=Math.max(maximumChunk,chunk.length);const result=await file!.write(chunk);expect(result.bytesWritten).toBe(chunk.length);offset+=chunk.length},commit:async()=>{await new Promise(r=>setTimeout(r,35));if(failCommit)throw Error('fixture disk commit failed');await file!.sync();await file!.close();file=undefined;await rename(stage,target);commits++},cancelReceive:async()=>{},cancel:async()=>{if(file){await file.close();file=undefined};if(reader){await reader.close();reader=undefined};await unlink(stage).catch(()=>{})},chooseUploads:async()=>({id:'selection',files:[{name:'selected.bin',size:data.length,modified:Date.now()}]}),read:async()=>{reader??=await open(source,'r');const chunk=Buffer.alloc(Math.min(32768,data.length-readOffset));const result=await reader.read(chunk,0,chunk.length,readOffset);expect(result.bytesRead).toBe(chunk.length);readOffset+=chunk.length;if(readOffset===data.length){await reader.close();reader=undefined};return{data:new Uint8Array(chunk),final:readOffset===data.length}}}
 const args=mode==='receive'?['sz','-be',remote+'/received.bin']:['rz','-b']
 const fingerprints=pty?['ed25519','ecdsa','rsa'].map(kind=>command(['exec',container,'ssh-keygen','-lf','/etc/ssh/ssh_host_'+kind+'_key.pub']).trim().split(/\s+/)[1]).join(','):''
 const child=pty?spawn(join(process.cwd(),'build','zmodem-pty.exe'),[mode,remote],{stdio:['pipe','pipe','pipe'],env:{...process.env,YAARE_OPENSSH_FINGERPRINT:fingerprints,YAARE_ZMODEM_SHELL:options.shell??'',YAARE_ZMODEM_NO_SFTP:options.noSFTP?'1':''}}):spawn(docker,['exec','-i','--user','yaaretest','--workdir',remote,container,...args],{stdio:['pipe','pipe','pipe']})
 let diagnostic='';child.stderr.on('data',data=>{diagnostic=(diagnostic+data.toString()).slice(-2048)})
 let ended=false;const exited=new Promise<number|null>((resolve,reject)=>{child.once('error',reject);child.once('exit',code=>{ended=true;resolve(code)})})
 let finalReplySent=false
 const finalBytes:number[]=[]
 const wire=async(bytes:Uint8Array)=>{
  if(mode==='receive'&&bytes.length>=6&&[42,42,24,66,48,56].every((value,index)=>bytes[index]===value))finalReplySent=true
  if(options.delay)await new Promise(r=>setTimeout(r,options.delay));await new Promise<void>((resolve,reject)=>child.stdin.write(Buffer.from(bytes),e=>e?reject(e):resolve()))}
 let engine:ZmodemEngine,peerPrompt:(()=>void)|undefined
 engine=new ZmodemEngine({read:async function*(){},send:()=>false,resize:()=>{},beginTransfer:async()=>'lease',writeTransfer:wire,onRemotePrompt:callback=>{peerPrompt=callback;return()=>{peerPrompt=undefined}},endTransfer:async abort=>{if(abort&&!ended)await wire(Uint8Array.from([24,24,24,24,24,24,24,24,8,8,8,8,8,8,8,8])).catch(()=>{});if(pty)child.stdin.end()}},'test',1,async()=>{},next=>{view=next;if(accepting)return;if(next.phase==='offer'){accepting=true;void engine.chooseFolder().then(()=>engine.receive()).finally(()=>{accepting=false})}else if(next.phase==='choose'){accepting=true;void engine.upload().finally(()=>{accepting=false})}},gateway)
 const timer=setTimeout(()=>child.kill(),options.bytes||options.delay?22000:9000)
 try{
  let carry='';const decoder=new TextDecoder();for await(const packet of child.stdout){if(!pty){await engine.consume(new Uint8Array(packet));continue};carry+=decoder.decode(packet,{stream:true});for(;;){const at=carry.indexOf('\n');if(at<0)break;const event=JSON.parse(carry.slice(0,at));carry=carry.slice(at+1);if(event.type==='output'&&event.data){
    const bytes=decodeBase64(event.data)
    if(finalReplySent&&finalBytes.length<2)finalBytes.push(...bytes.slice(0,2-finalBytes.length))
    await engine.consume(bytes)
   }else if(event.type==='directory'&&event.directory?.ready)peerPrompt?.()}}
  const code=await exited;await new Promise(r=>setTimeout(r,80));if(options.noSFTP)expect(diagnostic).toContain('SFTP_DENIED_CONFIRMED')
  if(failCommit){expect(code).not.toBe(0);expect(view?.phase).toBe('failed');expect(await readFile(target)).toEqual(original);expect(commits).toBe(0)}else{expect(code).toBe(0)
   if(pty&&mode==='receive'&&view?.phase==='unconfirmed'){
    expect(finalReplySent).toBe(true)
    expect(finalBytes).not.toEqual([79,79])
    expect(engine.active).toBe(false)
   }else expect(view?.phase,view?.error.slice(0,300)).toBe('completed')
   expect(view?.files).toBe(1);if(mode==='receive'){expect(await readFile(target)).toEqual(Buffer.from(data));expect(commits).toBe(1);expect(maximumChunk).toBeLessThanOrEqual(65536)}else{command(['cp',container+':'+remote+'/selected.bin',join(root,'remote-result.bin')]);expect(await readFile(join(root,'remote-result.bin'))).toEqual(Buffer.from(data))}}
 }finally{clearTimeout(timer);child.kill();await engine.dispose();if(/^\/home\/yaaretest\/files\/yaare-zmodem-[0-9a-f-]+$/.test(remote))command(['exec','--user','yaaretest',container,'rm','-rf',remote]);const expected=resolve(tmpdir())+require('node:path').sep+'yaare-zmodem-';if(!resolve(root).startsWith(expected))throw Error('unsafe test cleanup');await rm(root,{recursive:true,force:true})}
}
realTest('real lrzsz receive streams to disk before ACK and commits identical binary bytes',async()=>{await fixture('receive')},15000)
realTest('real lrzsz upload keeps binary data byte exact',async()=>{await fixture('send')},15000)
realTest('failed local commit sends cancellation instead of acknowledging completed file',async()=>{await fixture('receive',true)},15000)

realTest('real Session PTY receives ZMODEM binary without SFTP or terminal byte conversion',async()=>{await fixture('receive',false,true)},15000)
realTest('real Session PTY uploads ZMODEM binary without closing SSH early',async()=>{await fixture('send',false,true)},15000)

const matrixTest=process.env.YAARE_ZMODEM_MATRIX==='1'?test:test.skip
matrixTest('real SSH PTY transfers 16 MiB each way with throttled protocol writes',async()=>{
 await fixture('receive',false,true,{bytes:16*1024*1024,delay:2})
 await fixture('send',false,true,{bytes:16*1024*1024,delay:2})
},50000)
matrixTest('dash child shell and an SSH endpoint rejecting SFTP still transfer ZMODEM both ways',async()=>{
 await fixture('receive',false,true,{shell:'dash',noSFTP:true})
 await fixture('send',false,true,{shell:'dash',noSFTP:true})
},30000)
matrixTest('consecutive completed real PTY batches preserve bytes and leave no pending stage',async()=>{
 for(let n=0;n<3;n++){await fixture('receive',false,true,{bytes:2*1024*1024});await fixture('send',false,true,{bytes:2*1024*1024})}
},70000)
