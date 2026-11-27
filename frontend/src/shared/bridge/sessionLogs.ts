import {SSHService} from '../generated/mygo'
import {desktopAvailable} from './application'
export type SessionLog=Awaited<ReturnType<typeof SSHService.logList>>[number]
export type LogPage=Awaited<ReturnType<typeof SSHService.logRead>>
export const logGateway={
 available:desktopAvailable,
 list:()=>SSHService.logList(),
 start:(id:string,attempt:number)=>SSHService.logStart(id,attempt),
 stop:(id:string,attempt:number)=>SSHService.logStop(id,attempt),
 read:(id:string,text:string,after:number,cursor:number)=>SSHService.logRead({id,text,after,cursor}),
 export:(id:string)=>SSHService.logExport(id),
 delete:(id:string)=>SSHService.logDelete(id),
}
