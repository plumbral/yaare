import { tr } from '../i18n/messages'
import { useLocale, getLanguage } from '../i18n/locale'
import { Channel } from 'mygo-runtime'
import { SSHService, type SSHConnectRequest, type Event } from '../generated/mygo'
import type { TerminalTransport } from '../../features/terminal/remoteOutput'
import { decodeBase64,encodeBase64 as base64 } from './base64'

export type SSHEvent = Event
const resizeIntervalMs = 100
export const broadcastGateway={available:()=>typeof window!=='undefined'&&!!window.mygo,send:SSHService.broadcast,status:SSHService.broadcastStatus}
export interface SSHTransport extends TerminalTransport {
  close: () => void
  confirm: (id: string, decision: 'once' | 'remember' | 'replace' | 'reject') => Promise<boolean>
  answerAuthentication: (id:string,answers:string[])=>Promise<boolean>
  setForward:(ruleId:string,start:boolean)=>Promise<boolean>
  changeDirectory:(path:string)=>Promise<void>
  sendBinary: (data: string) => boolean
}

export function createSSHTransport(request: SSHConnectRequest, onEvent: (event: SSHEvent) => void, onOutput?: () => void): SSHTransport {
  const channel = new Channel<SSHEvent>()
  const { sessionId, attempt } = request
  let closed = false, writable = false, consumed = false, ended = false, connected = false, queued = 0, revision = 0
  let dimensions = { columns: 80, rows: 24 }
  let sentDimensions: typeof dimensions | undefined
  let resizeTimer: ReturnType<typeof setTimeout> | undefined
  let resizeInFlight = false
  let writes = Promise.resolve()
  let transferLease='',transferPending=false,transferWrites=Promise.resolve(),transferBegin:Promise<string>|null=null
  let remotePrompt:(()=>void)|undefined
  const clearResize = () => {
    clearTimeout(resizeTimer)
    resizeTimer = undefined
  }
  const closeChannel = () => {
    if (closed) return
    closed = true
    writable = false
    clearResize()
    channel.close()
  }
  const canResize = () => writable && !closed && !resizeInFlight &&
    (sentDimensions?.columns !== dimensions.columns || sentDimensions?.rows !== dimensions.rows)
  const sendResize = () => {
    clearResize()
    if (!canResize()) return
    sentDimensions = dimensions
    resizeInFlight = true
    void SSHService.resize(sessionId, attempt, dimensions.columns, dimensions.rows).catch(() => {}).finally(() => {
      resizeInFlight = false
      scheduleResize()
    })
  }
  const scheduleResize = () => {
    if (resizeTimer === undefined && canResize()) resizeTimer = setTimeout(sendResize, resizeIntervalMs)
  }
  const publish = (event: SSHEvent) => {
    if (closed || ended || !Number.isSafeInteger(event.revision) || event.revision <= revision) return
    revision = event.revision
    const firstConnection = event.type === 'connected' && !connected
    if (firstConnection) connected = writable = true
    if (event.type === 'failed' || event.type === 'disconnected') { writable = false; ended = true; clearResize() }
    if(event.type==='directory'&&event.directory?.ready&&(transferPending||transferLease))remotePrompt?.()
    onEvent(event)
    if (firstConnection) sendResize()
  }
  const fail = (code: string, message: string) => {
    if (closed) return
    publish({ type: 'failed', revision: revision + 1, code, message })
    closeChannel()
  }
  const connectionLost = () => fail('CONNECTION_LOST', tr("远程数据通道已结束。"))
  const finished = SSHService.connect(request, channel).catch(() => {
    if (connected) connectionLost()
    else fail('BRIDGE_FAILED', tr("SSH 桥接请求失败，或连接数已达上限。请重试。"))
  })
  request.password = ''; request.passphrase = ''; request.privateKey = ''
  const sendBytes = (bytes: Uint8Array) => {
    if(transferPending||transferLease)return false
    if (closed || !writable || !bytes.length) return false
    if (queued + bytes.length > 262144) {
      fail('INPUT_OVERFLOW', tr("待发送内容超过 256 KB，连接已停止以避免丢失输入。请分段粘贴。")); return false
    }
    queued += bytes.length
    writes = writes.then(async () => {
      try {
        for (let i = 0; i < bytes.length && !closed; i += 32768) {
          if (!await SSHService.write(sessionId, attempt, base64(bytes.subarray(i, i + 32768)))) throw new Error('write rejected')
        }
      } catch { connectionLost() }
      finally { queued -= bytes.length; bytes.fill(0) }
    })
    return true
  }
  return {
    async *read(signal) {
      if (consumed || closed) return
      consumed = true
      const abort = closeChannel
      if (signal.aborted) { abort(); return }
      signal.addEventListener('abort', abort, { once: true })
      try {
        for await (const event of channel) {
          if (closed || signal.aborted) break
          if (event.type === 'output' && event.data) {
            const bytes = decodeBase64(event.data)
            if (writable && bytes.length) onOutput?.()
            yield bytes
          } else publish(event)
        }
        await finished
        if (!ended && !closed) connectionLost()
      } catch { connectionLost()
      } finally { signal.removeEventListener('abort', abort); closeChannel() }
    },
    send(data) {
      if (data.length > 262144) { fail('INPUT_OVERFLOW', tr("粘贴内容过大，请分段发送。")); return false }
      return sendBytes(new TextEncoder().encode(data))
    },
    sendBinary(data) { return sendBytes(Uint8Array.from(data, char => char.charCodeAt(0))) },
    async beginTransfer(){
      if(closed||!writable||transferPending||transferLease)throw Error(tr("终端传输尚未就绪。"))
      transferPending=true
      try{transferBegin=(async()=>{await writes;if(closed||!writable)throw Error(tr("SSH 已断开。"));transferLease=await SSHService.terminalTransferBegin(sessionId,attempt);return transferLease})();return await transferBegin}catch(cause){transferPending=false;throw cause}finally{transferBegin=null}
    },
    async writeTransfer(data){
      const token=transferLease;if(!token||closed||!writable||data.byteLength>65536)throw Error(tr("终端文件传输已结束。"))
      const sending=transferWrites.then(async()=>{if(transferLease!==token||closed)throw Error(tr("终端传输已取消。"));if(!await SSHService.terminalTransferWrite(sessionId,attempt,token,base64(data)))throw Error(tr("终端传输未确认，请检查连接。"))}).finally(()=>data.fill(0))
      transferWrites=sending.catch(()=>{});await sending
    },
    async endTransfer(abort){await transferBegin?.catch(()=>{});const token=transferLease;transferLease='';try{if(token)await SSHService.terminalTransferEnd(sessionId,attempt,token,abort)}finally{transferPending=false}},
    onRemotePrompt(callback){remotePrompt=callback;return()=>{if(remotePrompt===callback)remotePrompt=undefined}},
    async startZmodemUpload(){if(closed||!writable||transferPending||transferLease)throw Error(tr("终端当前不可用于文件上传。"));await writes;await SSHService.startZmodemUpload(sessionId,attempt)},
    resize(columns, rows) {
      if (closed || ended) return
      dimensions = { columns, rows }
      scheduleResize()
    },
    async confirm(id, decision) {
      if (closed) return false
      try { return await SSHService.confirmHost(sessionId, attempt, id, decision) } catch { return false }
    },
    async answerAuthentication(id,answers){if(closed)return false;try{return await SSHService.answerAuthentication(sessionId,attempt,id,answers)}catch{return false}finally{answers.fill('')}},
    async setForward(ruleId,start){if(closed||!writable)return false;try{return await SSHService.setForward(sessionId,attempt,ruleId,start)}catch{return false}},
    async changeDirectory(path){if(closed||!writable)throw Error(tr("SSH 连接已断开。"));await writes;if(closed||!writable)throw Error(tr("SSH 连接已断开。"));await SSHService.changeDirectory(sessionId,attempt,path)},
    close() {
      if (!closed) { publish({ type: 'disconnected', revision: revision + 1, code: 'CANCELLED', message: tr("连接已断开。") }); closeChannel() }
    },
  }
}
