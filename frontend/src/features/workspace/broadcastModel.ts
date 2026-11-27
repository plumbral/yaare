import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import {commandInput,normalizeCommandText} from './shortcuts'
export interface BroadcastOption{sessionId:string;attempt:number;label:string;description:string}
export class BroadcastDraftError extends Error{constructor(public field:'targets'|'command',message:string){super(message)}}
export function prepareBroadcast(options:readonly BroadcastOption[],selected:readonly string[],command:string,execute:boolean){
 const unique=new Set(selected);if(!unique.size||unique.size>8||unique.size!==selected.length)throw new BroadcastDraftError('targets',tr("请选择当前标签中的 1–8 个目标窗格。"))
 const targets=options.filter(option=>unique.has(option.sessionId)).map(option=>({...option}))
 if(targets.length!==unique.size||targets.some(target=>target.attempt<1))throw new BroadcastDraftError('targets',tr("所选目标已断开或移出当前标签，请重新选择。"))
 const normalized=normalizeCommandText(command),input=commandInput(normalized,execute)
 if(input===null||new TextEncoder().encode(input).length>65536)throw new BroadcastDraftError('command',execute?tr("请输入不超过 64 KiB 的命令，不包含其他控制字符。"):tr("仅填入模式只支持单行命令，不发送回车。"))
 return{targets,command:normalized,execute}
}
