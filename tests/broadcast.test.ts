import {expect,test} from 'bun:test'
import {prepareBroadcast} from '../frontend/src/features/workspace/broadcastModel'
test('broadcast requires explicit selection and freezes each connection attempt',()=>{
 const options=Array.from({length:8},(_,i)=>({sessionId:'p'+i,attempt:i+1,label:'窗格 '+(i+1),description:'fixture'}))
 expect(()=>prepareBroadcast(options,[],'pwd',true)).toThrow();expect(()=>prepareBroadcast(options,['p0','p0'],'pwd',true)).toThrow();expect(()=>prepareBroadcast(options,['other-tab'],'pwd',true)).toThrow()
 const preview=prepareBroadcast(options,['p7','p0'],'  pwd\r\nwhoami',true);options[0]!.attempt=50;expect(preview.targets.map(t=>t.sessionId)).toEqual(['p0','p7']);expect(preview.targets[0]!.attempt).toBe(1);expect(preview.command).toBe('  pwd\nwhoami')
 expect(()=>prepareBroadcast(options,['p0'],'pwd\nwhoami',false)).toThrow();expect(()=>prepareBroadcast(options,['p0'],'a'.repeat(65536),true)).toThrow()
})
