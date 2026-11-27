import {expect,test} from 'bun:test'
import {matchNativeDrop,type DropDestination} from '../frontend/src/features/workspace/nativeDrop'
test('native grant matches the frozen DOM destination and never a newly active pane',()=>{
 const target:DropDestination={x:20,y:30,at:1000,location:{side:'remote',path:'/original',sessionId:'pane-a',attempt:1,token:''}},drop={paths:['yaare-drop:12345678-abcd-1234'],x:20,y:30}
 expect(matchNativeDrop(drop,target,1500)?.location.sessionId).toBe('pane-a');expect(matchNativeDrop(drop,target,1500)?.location.path).toBe('/original')
 expect(matchNativeDrop(drop,null,1500)).toBeNull();expect(matchNativeDrop(drop,target,7000)).toBeNull();expect(matchNativeDrop({...drop,x:100},target,1500)).toBeNull();expect(matchNativeDrop({...drop,paths:['C:\\private\\file']},target,1500)).toBeNull()
})
