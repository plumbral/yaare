import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import { useEffect } from 'react'
import { discardKeySelection, type KeyReference, type KeySelection } from '../../shared/bridge/keys'
import type { HostProfile } from '../workspace/mockGateway'
import type { Credentials } from './useSSHSessions'

export interface AuthenticationDraft {
  authentication: 'password' | 'key'; keySource: 'file' | 'text' | 'agent' | 'generated'; keyId: string
  password: string; privateKey: string; passphrase: string
  generatedKey?: KeySelection
  temporaryFile?: {token:string;name:string}
}
export interface ConnectionPasswordSave { rememberPassword: boolean; password: string; privateKey?: string; passphrase?: string; keyToken?: string; generatedKeyToken?: string; proxyPassword?: string }
export function useGeneratedKeyCleanup(token?: string) {
  useEffect(() => () => {
    if (token) void discardKeySelection(token).catch(() => {})
  }, [token])
}
export function sameAuthenticationSource(a: HostProfile, b: HostProfile) {
  return (a.authentication ?? 'none') === (b.authentication ?? 'none') &&
    (a.identityId ?? '') === (b.identityId ?? '') && (a.keyId ?? '') === (b.keyId ?? '') && !!a.useAgent === !!b.useAgent
}
export function sameCredentialTarget(a: HostProfile, b: HostProfile) {
  return a.id === b.id && a.address.trim() === b.address.trim() && a.port === b.port &&
    a.username.trim() === b.username.trim() && sameAuthenticationSource(a, b)
}
export const hasSavedPassword = (host: HostProfile | undefined, candidate: HostProfile) =>
  !!host?.hasCredential && candidate.authentication === 'password' && !candidate.identityId && sameCredentialTarget(host, candidate)
export function savedConnectionCredentials(host: HostProfile): Credentials | undefined {
  if (!host.identityId && !host.hasCredential && !(host.authentication === 'key' && (host.keyId || host.useAgent))) return undefined
  return { authentication: host.authentication === 'key' ? 'key' : 'password', identityId: host.identityId ?? '', keyId: host.identityId ? '' : host.keyId ?? '', useAgent: !host.identityId && !!host.useAgent, useSavedCredential: !!host.hasCredential && (!host.identityId || host.authentication==='key'), password: '', passphrase: '', timeoutSeconds: host.timeoutSeconds || 15 }
}
export function needsAuthenticationInput(code: string): boolean {
  return ['AUTHENTICATION_FAILED','AUTH_REQUIRED','CREDENTIAL_UNAVAILABLE','IDENTITY_UNAVAILABLE',
    'KEY_PASSPHRASE_REQUIRED','KEY_DECRYPT_FAILED','KEY_UNAVAILABLE','KEY_INVALID','AGENT_UNAVAILABLE'].includes(code)
}
export function reusableConnectionCredentials(credentials:Credentials):Credentials|undefined {
  if(!credentials.useSavedCredential&&!credentials.identityId&&!(credentials.authentication==='key'&&(credentials.keyId||credentials.useAgent)))return undefined
  return {authentication:credentials.authentication,identityId:credentials.identityId,keyId:credentials.keyId,useAgent:credentials.useAgent,useSavedCredential:credentials.useSavedCredential,password:'',passphrase:'',timeoutSeconds:credentials.timeoutSeconds}
}
export const emptyAuthentication = (host: Pick<HostProfile, 'authentication' | 'keyId' | 'useAgent'>): AuthenticationDraft => ({
  authentication: host.authentication === 'key' ? 'key' : 'password', keySource: host.useAgent ? 'agent' : 'file', keyId: host.keyId ?? '',
  password: '', privateKey: '', passphrase: '',
})
export const clearAuthentication = (draft: AuthenticationDraft): AuthenticationDraft => ({ ...draft, password: '', privateKey: '', passphrase: '' })
export function authenticationError(draft: AuthenticationDraft, keys: KeyReference[]): string {
  if (draft.password.length > 16384 || draft.passphrase.length > 16384) return tr("认证内容过长，请检查输入。")
  if (draft.authentication === 'key') {
    if (draft.keySource === 'generated' && !draft.generatedKey) return tr("请重新生成密钥。")
    if (draft.keySource === 'file' && !draft.temporaryFile && !keys.some(key => key.id === draft.keyId && key.available)) return tr("请选择一个仍然可用的密钥。")
    if (draft.keySource === 'text' && (!draft.privateKey.trim() || draft.privateKey.length > 1048576)) return tr("请粘贴有效的私钥内容，最大 1 MB。")
  }
  return ''
}
export function credentialsFromDraft(draft: AuthenticationDraft, timeoutSeconds = 15): Credentials {
  return { authentication: draft.authentication, timeoutSeconds,
    useAgent: draft.authentication === 'key' && draft.keySource === 'agent',
    keyId: draft.authentication === 'key' && draft.keySource === 'file' ? draft.keyId : '',
    temporaryKeyToken: draft.authentication==='key'&&draft.keySource==='file'?draft.temporaryFile?.token:undefined,
    privateKey: draft.authentication === 'key' && draft.keySource === 'text' ? draft.privateKey.trim() : '',
    password: draft.authentication === 'password' ? draft.password : '', passphrase: draft.authentication === 'key' && draft.keySource !== 'agent' ? draft.passphrase : '' }
}
