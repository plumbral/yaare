import {expect,test} from 'bun:test'
import {defaultPreferences,normalizePreferences,readPreferences,savePreferences} from '../frontend/src/features/workspace/preferences'

test('directory linking is opt-in and only the boolean preference persists',()=>{
 expect(defaultPreferences.directorySync).toBe(false)
 expect(normalizePreferences({directorySync:'true'}).directorySync).toBe(false)
 const descriptor=Object.getOwnPropertyDescriptor(globalThis,'localStorage'),memory=new Map<string,string>()
 Object.defineProperty(globalThis,'localStorage',{configurable:true,value:{getItem:(key:string)=>memory.get(key)??null,setItem:(key:string,value:string)=>memory.set(key,value)}})
 try{
  expect(savePreferences(normalizePreferences({directorySync:true,cwd:'/private/path',password:'secret'}))).toBe(true)
  expect(readPreferences().directorySync).toBe(true)
  expect(JSON.parse(memory.get('yaare.preview.preferences.v1')!)).not.toHaveProperty('cwd')
  expect(JSON.parse(memory.get('yaare.preview.preferences.v1')!)).not.toHaveProperty('password')
  expect(savePreferences({...readPreferences(),directorySync:false})).toBe(true)
  expect(readPreferences().directorySync).toBe(false)
 }finally{if(descriptor)Object.defineProperty(globalThis,'localStorage',descriptor);else Reflect.deleteProperty(globalThis,'localStorage')}
})
