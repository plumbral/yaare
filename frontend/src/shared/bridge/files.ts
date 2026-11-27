import { Channel } from 'mygo-runtime'
import {
  FileService,
  type FileLocation,
  type FileDirectory,
  type FileEvent,
  type FileOperation,
  type FileTask,
  type LocalFolder,
  type TransferRequest,
  type TextFile,
  type TextSaveRequest,
  type TextSaveResult,
} from '../generated/mygo'
import { desktopAvailable, supportsDesktopRecovery } from './application'
export type { FileLocation, FileDirectory, FileOperation, FileTask, LocalFolder, TransferRequest }
export type {TransferRecoveryView,RecoverableTransfer,RestoreTransferRequest} from '../generated/mygo'
export type { TextFile, TextSaveRequest, TextSaveResult }
export type {SearchHit,SearchProgress} from '../generated/mygo'
export type {ComparisonView,CompareEntry,SyncIntent,ComparedText} from '../generated/mygo'
export type {DirectorySyncView,DirectorySyncResolution,BilateralAction} from '../generated/mygo'
export type {FileConnection} from '../generated/mygo'
export type FileEntry = FileDirectory['entries'][number]
export const fileGateway = {
  available: desktopAvailable,
  canRecoverTransfers: supportsDesktopRecovery,
  selectLocal: () => FileService.selectLocal(),
  openLocalHome: () => FileService.openLocalHome(),
  fileConnections:()=>FileService.fileConnections(),
  list: (location: FileLocation) => FileService.list(location),
  operate: (request: FileOperation) => FileService.operate(request),
  readText: (location: FileLocation,encoding='auto') => FileService.readTextWithEncoding(location,encoding),
  saveText: (request: TextSaveRequest) => FileService.saveText(request),
  setTextProtection:(revision:number,protectedValue:boolean)=>FileService.setTextProtection(revision,protectedValue),
  transfer: (request: TransferRequest) => FileService.transfer(request),
  uploadDropped:(token:string,destination:FileLocation)=>FileService.uploadDropped(token,destination),
  compare(request:import('../generated/mygo').ComparisonRequest,onProgress:(event:import('../generated/mygo').ComparisonEvent)=>void,onError:(error:unknown)=>void,onDone:()=>void){
    const channel=new Channel<import('../generated/mygo').ComparisonEvent>();let closed=false
    void FileService.compare(request,channel).catch(error=>{if(!closed)onError(error)}).finally(()=>{if(!closed)onDone()})
    void(async()=>{try{for await(const event of channel){if(!closed)onProgress(event)}}catch(error){if(!closed)onError(error)}})()
    return()=>{closed=true;channel.close()}
  },
  forgetComparison:(id:string)=>FileService.forgetComparison(id),
  comparisonText:(id:string,relative:string,leftEncoding='auto',rightEncoding='auto')=>FileService.comparisonText(id,relative,leftEncoding,rightEncoding),
  previewSync:(id:string,direction:string,selected:string[])=>FileService.previewSync(id,direction,selected),
  executeSync:(id:string)=>FileService.executeSync(id,true),
  discardSync:(id:string)=>FileService.discardSync(id),
  previewDirectorySync:(id:string,deletes:boolean)=>FileService.previewDirectorySync(id,deletes),
  syncConflictText:(id:string,path:string,revision:number)=>FileService.syncConflictText(id,path,revision),
  resolveDirectorySync:(request:import('../generated/mygo').DirectorySyncResolution)=>FileService.resolveDirectorySync(request),
  stopDirectorySync:(sessionId:string)=>FileService.stopDirectorySync(sessionId),
  directorySync(request:import('../generated/mygo').DirectorySyncRequest,onProgress:(view:import('../generated/mygo').DirectorySyncView)=>void,onError:(error:unknown)=>void,onDone:()=>void){
    const channel=new Channel<import('../generated/mygo').DirectorySyncView>();let closed=false
    void FileService.directorySync(request,channel).catch(error=>{if(!closed)onError(error)}).finally(()=>{if(!closed)onDone()})
    void(async()=>{try{for await(const view of channel){if(!closed)onProgress(view)}}catch(error){if(!closed)onError(error)}})()
    return()=>{closed=true;channel.close()}
  },
  search(location:FileLocation,query:string,hidden:boolean,recursive:boolean,onProgress:(event:import('../generated/mygo').SearchProgress)=>void,onError:(error:unknown)=>void,onDone:()=>void){
    const channel=new Channel<import('../generated/mygo').SearchProgress>();let closed=false
    void FileService.search({location,query,hidden,recursive},channel).catch(error=>{if(!closed)onError(error)}).finally(()=>{if(!closed)onDone()})
    void(async()=>{try{for await(const event of channel){if(!closed)onProgress(event)}}catch(error){if(!closed)onError(error)}})()
    return()=>{closed=true;channel.close()}
  },
  control: (id: string, action: string) => FileService.control(id, action),
  resolveConflict: (id: string, conflictId: string, action: string) => FileService.resolveConflict(id, conflictId, action),
  tasks: () => FileService.tasks(),
  recoverableTransfers:()=>FileService.recoverableTransfers(),
  restoreTransfer:(request:import('../generated/mygo').RestoreTransferRequest)=>FileService.restoreTransfer(request),
  discardRecoveredTransfer:(id:string)=>FileService.discardRecoveredTransfer(id),
  authorizeRecoveredDrop:(id:string,token:string)=>FileService.authorizeRecoveredDrop(id,token),
  chooseRecoveredSources:(id:string)=>FileService.chooseRecoveredSources(id),
  controlMany: (ids: string[], action: string) => FileService.controlMany(ids, action),
  clearCompleted: () => FileService.clearCompleted(),
  configureTransfers: (concurrency: number) => FileService.configureTransfers(concurrency),
  watch(onTasks: (tasks: FileTask[]) => void, onError: () => void) {
    const channel = new Channel<FileEvent>()
    let closed = false
    void FileService.watch(channel).catch(() => {
      if (!closed) onError()
    })
    void (async () => {
      try {
        for await (const event of channel) {
          if (closed) break
          onTasks(event.tasks)
        }
      } catch {
        if (!closed) onError()
      }
    })()
    return () => {
      closed = true
      channel.close()
    }
  },
}
