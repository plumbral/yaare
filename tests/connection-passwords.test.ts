import {expect,test} from 'bun:test'
import {committedConnection,sameConnectionTarget} from '../frontend/src/features/connections/useHostSessions'
import {hasSavedPassword,sameCredentialTarget,savedConnectionCredentials,reusableConnectionCredentials,clearAuthentication,emptyAuthentication,needsAuthenticationInput} from '../frontend/src/features/connections/authenticationDraft'
import type {HostProfile} from '../frontend/src/features/workspace/mockGateway'

const host:HostProfile={id:'saved',name:'Saved host',address:'127.0.0.1',port:22222,username:'deploy',group:'未分组',system:'Linux',authentication:'password',hasCredential:true}
test('save and connect uses the key assigned by its own commit and checks newer snapshots before dialing', () => {
 const saved: HostProfile = {...host, authentication:'key', keyId:'assigned-by-vault',jumpHostIds:['jump']}
 const jump = {...host, id:'jump'}
 const receipt = [saved,jump]
 expect(committedConnection(host.id,receipt,[{...saved,name:'Updated label'},jump])?.keyId).toBe('assigned-by-vault')
 expect(committedConnection(host.id,receipt,[{...saved,keyId:'other'},jump])).toBeUndefined()
 expect(committedConnection(host.id,receipt,[saved,{...jump,address:'new-jump.example.test'}])).toBeUndefined()
 expect(committedConnection(host.id,receipt,[jump])).toBeUndefined()
})

test('a session matches its public destination, authentication source, proxy and ordered jump chain', () => {
 const target: HostProfile = { ...host, jumpHostIds: ['first','second'], proxy: {kind:'socks5',address:'proxy.example.test',port:1080,useAuthentication:true,username:'agent'} }
 expect(sameConnectionTarget(target, {...target, name:'New label', notes:'Updated notes'})).toBe(true)
 for (const change of [{address:'second.example.test'}, {username:'other'}, {port:22}, {authentication:'key' as const},
   {identityId:'different'}, {jumpHostIds:['second','first']}, {proxy:{...target.proxy!,port:1081}},
   {proxy:{...target.proxy!,username:'second'}}, {proxy:undefined}]) {
   expect(sameConnectionTarget(target, {...target,...change})).toBe(false)
 }
 expect(sameConnectionTarget(host, {...host, proxy:{kind:'none',address:'',port:0,useAuthentication:false,username:''}})).toBe(true)
 expect(sameConnectionTarget(host, {...host, proxy:{kind:'auto',address:'',port:0,useAuthentication:false,username:''}})).toBe(false)
})

test('saved connection requests carry only a reference and remain reusable after reopening',()=>{
 const reopened=JSON.parse(JSON.stringify(host)) as HostProfile
 const credentials=savedConnectionCredentials(reopened)!
 expect(credentials.useSavedCredential).toBe(true);expect(credentials.password).toBe('');expect(credentials.privateKey).toBeUndefined();expect(credentials.identityId).toBe('')
 expect(savedConnectionCredentials({...host,hasCredential:false})).toBeUndefined()
 const draft=emptyAuthentication(host);draft.password='temporary input';expect(clearAuthentication(draft).password).toBe('');expect(savedConnectionCredentials(host)!.password).toBe('')
})
test('saved-password availability follows the same account and destination, with shared identities independent',()=>{
 expect(hasSavedPassword(host,{...host,name:'renamed'})).toBe(true)
 for(const patch of [{username:'other'},{address:'192.0.2.1'},{port:22},{authentication:'key' as const},{identityId:'shared'}])expect(hasSavedPassword(host,{...host,...patch})).toBe(false)
 const shared=savedConnectionCredentials({...host,identityId:'shared'})!
 expect(shared.identityId).toBe('shared');expect(shared.useSavedCredential).toBe(false)
 const key=savedConnectionCredentials({...host,hasCredential:false,authentication:'key',keyId:'key-one'})!
 expect(key.keyId).toBe('key-one');expect(key.useSavedCredential).toBe(false)
})
test('automatic reconnect keeps saved references and discards all one-shot secret material',()=>{
 const credentials={...savedConnectionCredentials(host)!,password:'one-shot password',passphrase:'one-shot phrase',privateKey:'one-shot private key'}
 const retry=reusableConnectionCredentials(credentials)!
 expect(retry.useSavedCredential).toBe(true);expect(retry.password).toBe('');expect(retry.passphrase).toBe('');expect(retry.privateKey).toBeUndefined()
 expect(reusableConnectionCredentials({...credentials,useSavedCredential:false})).toBeUndefined()
 expect(reusableConnectionCredentials({...credentials,useSavedCredential:false,identityId:'shared'})!.identityId).toBe('shared')
})
test('credential problems can prompt for input while network and host key errors cannot',()=>{
 for(const code of ['AUTHENTICATION_FAILED','CREDENTIAL_UNAVAILABLE','KEY_PASSPHRASE_REQUIRED','KEY_UNAVAILABLE','IDENTITY_UNAVAILABLE'])expect(needsAuthenticationInput(code)).toBe(true)
 for(const code of ['','CONNECTION_REFUSED','CONNECT_TIMEOUT','HOST_KEY_CHANGED','AUTH_TIMEOUT','PROXY_AUTH_FAILED'])expect(needsAuthenticationInput(code)).toBe(false)
})

test('clearing submitted plaintext keeps the page-owned generated key draft available for a retry', () => {
  const generatedKey = { token: 'page-draft', name: 'key', format: 'OpenSSH' }
  const draft = { ...emptyAuthentication(host), keySource: 'generated' as const, generatedKey, passphrase: 'temporary' }
  const cleared = clearAuthentication(draft)
  expect(cleared.generatedKey).toBe(generatedKey)
  expect(cleared.passphrase).toBe('')
  expect(cleared.privateKey).toBe('')
})

test('an authentication form opened before a change cannot overwrite the new credential source', () => {
  const sharedKey = { ...host, authentication: 'key' as const, keyId: 'key-one' }
  expect(sameCredentialTarget(host, { ...host, notes: 'new notes', identityId: '', useAgent: false })).toBe(true)
  for (const updated of [{ ...host, id: 'another' }, { ...host, port: 2022 }, { ...host, identityId: 'shared' },
    { ...sharedKey, keyId: 'key-two' }, { ...host, authentication: 'key' as const, useAgent: true }]) {
    expect(sameCredentialTarget(host, updated)).toBe(false)
  }
  expect(sameCredentialTarget(sharedKey, { ...sharedKey, keyId: 'key-two' })).toBe(false)
  expect(hasSavedPassword(sharedKey, host)).toBe(false)
})
