export interface FileCloseState {
  dirty: boolean
  busy: boolean
  path?: string
  revision?: number
  syncing?: boolean
}
export interface FileCloseGuard {
  getCloseState(): FileCloseState
  saveForClose(): Promise<boolean>
  reveal(): void
  cancelForClose?(): Promise<void>
}
export type RegisterFileGuard=(sessionId:string,key:string,guard:FileCloseGuard|null)=>void
