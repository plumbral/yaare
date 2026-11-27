import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
export const MAX_REMOTE_TEXT_BYTES = 2 * 1024 * 1024
export function remoteTextError(value: string) {
  return new TextEncoder().encode(value).length > MAX_REMOTE_TEXT_BYTES ? tr("编辑内容超过 2 MiB，请减少内容后再保存。") : ''
}
export function textLanguage(path: string) {
  const name = path.split('/').pop()?.toLowerCase() ?? ''
  const extension = name.split('.').pop() ?? ''
  if (/^(dockerfile|containerfile)(\.|$)/.test(name)) return 'dockerfile'
  if (['.bashrc','.bash_profile','.profile','.zshrc'].includes(name)) return 'shell'
  if (name === '.env' || name.startsWith('.env.')) return 'properties'
  if (name.includes('nginx') && extension === 'conf') return 'nginx'
  return ({sh:'shell',bash:'shell',zsh:'shell',json:'json',jsonc:'json',js:'javascript',jsx:'javascript',mjs:'javascript',ts:'typescript',tsx:'typescript',py:'python',yml:'yaml',yaml:'yaml',toml:'toml',ini:'properties',conf:'properties',properties:'properties',css:'css',xml:'xml',html:'html',htm:'html',go:'go',sql:'sql',ps1:'powershell'} as Record<string,string>)[extension] ?? 'text'
}

export function fileLocation(sessionId:string,attempt:number,path:string) {
  return {side:'remote',token:'',path,sessionId,attempt}
}
