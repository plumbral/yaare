import {FileService,type TerminalFileScope} from '../generated/mygo'
import {encodeBase64,decodeBase64} from './base64'
export type {TerminalFileScope,TerminalReceiveView,TerminalUploadSelection,LocalFolder} from '../generated/mygo'
export const terminalFileGateway={
 chooseFolder:(scope:TerminalFileScope)=>FileService.terminalReceiveChooseFolder(scope),
 prepare:FileService.terminalReceivePrepare,
 resolve:FileService.terminalReceiveResolve,
 write:(id:string,offset:number,data:Uint8Array)=>FileService.terminalReceiveWrite(id,offset,encodeBase64(data)),
 commit:FileService.terminalReceiveCommit,
 cancelReceive:FileService.terminalReceiveCancel,
 chooseUploads:(scope:TerminalFileScope)=>FileService.terminalUploadsChoose(scope),
 async read(id:string,name:string){const chunk=await FileService.terminalUploadRead(id,name);return{data:decodeBase64(chunk.data),final:chunk.final}},
 cancel:FileService.terminalFilesCancel,
}
