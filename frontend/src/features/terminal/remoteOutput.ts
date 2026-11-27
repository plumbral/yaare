export interface TerminalTransport {
  read: (signal: AbortSignal) => AsyncIterable<Uint8Array>
  send: (data: string) => boolean
  sendBinary?: (data: string) => boolean
  resize: (columns: number, rows: number) => void
  beginTransfer?:()=>Promise<string>
  writeTransfer?:(data:Uint8Array)=>Promise<void>
  endTransfer?:(abort:boolean)=>Promise<void>
  startZmodemUpload?:()=>Promise<void>
  onRemotePrompt?:(callback:()=>void)=>(()=>void)
}
export async function writeRemotePacket(packet: Uint8Array, write: (bytes: Uint8Array, done: () => void) => void, cancelled: () => boolean) {
  for (let offset = 0; offset < packet.byteLength && !cancelled(); offset += 65536) {
    await new Promise<void>(resolve => write(packet.subarray(offset, Math.min(offset + 65536, packet.byteLength)), resolve))
  }
}
