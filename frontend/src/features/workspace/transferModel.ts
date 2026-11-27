import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import type { FileTask } from '../../shared/bridge/files'
export const transferLabels:Record<string,string>={get queued() { return tr("排队中") },get running() { return tr("传输中") },get pausing() { return tr("正在暂停") },get paused() { return tr("已暂停") },get conflict() { return tr("等待处理冲突") },get cancelling() { return tr("正在取消") },get completed() { return tr("已完成") },get failed() { return tr("传输失败") },get cancelled() { return tr("已取消") }}
export const phaseLabels:Record<string,string>={get planning() { return tr("准备文件") },get checking() { return tr("校验文件") },get committing() { return tr("保存文件") },get transferring() { return tr("传输中") }}
export const canPause=(t:FileTask)=>t.state==='queued'||t.state==='running'&&t.protocol!=='scp'
export const canContinue=(t:FileTask)=>t.canResume&&(t.state==='paused'||t.state==='failed')
export const canRetry=(t:FileTask)=>!t.reviewed&&(t.state==='failed'||t.state==='cancelled')
export function remainingTime(seconds:number){if(seconds<0||!Number.isFinite(seconds))return '';if(seconds<60)return tr("剩余 {p0} 秒", Math.max(1,Math.ceil(seconds)));if(seconds<3600)return tr("剩余 {p0} 分钟", Math.ceil(seconds/60));return tr("剩余 {p0} 小时", (seconds/3600).toFixed(1))}
export function transferSize(bytes:number){if(bytes<1024)return Math.max(0,bytes)+' B';const unit=Math.min(4,Math.floor(Math.log(bytes)/Math.log(1024)));return (bytes/1024**unit).toFixed(1)+' '+['B','KB','MB','GB','TB'][unit]}

export function transfersForClose(tasks: readonly FileTask[], sessionIds: readonly string[]): FileTask[] {
  const sessions = new Set(sessionIds)
  const affected = new Map<string, FileTask>()
  for (const task of tasks) {
    if (task.state === 'completed' || task.state === 'cancelled') continue
    if ([task.sessionId, task.sourceSession, task.destinationSession].some(id => id && sessions.has(id))) {
      affected.set(task.id, task)
    }
  }
  return [...affected.values()]
}
