import {expect,test} from 'bun:test'
import {emptyAuthentication,credentialsFromDraft,clearAuthentication,authenticationError} from '../frontend/src/features/connections/authenticationDraft'

test('Agent profiles restore the source and produce no manual credentials',()=>{
 const draft=emptyAuthentication({authentication:'key',useAgent:true,keyId:''})
 expect(draft.keySource).toBe('agent');expect(authenticationError(draft,[])).toBe('')
 const request=credentialsFromDraft({...draft,password:'old',privateKey:'old',passphrase:'old'})
 expect(request).toMatchObject({useAgent:true,keyId:'',password:'',privateKey:'',passphrase:''})
 expect(clearAuthentication({...draft,password:'old'})).toMatchObject({keySource:'agent',password:''})
 expect(credentialsFromDraft({...draft,authentication:'password'}).useAgent).toBe(false)
 expect(emptyAuthentication({authentication:'key',keyId:'file'}).keySource).toBe('file')
})
