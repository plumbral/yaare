import { transfersForClose } from '../frontend/src/features/workspace/transferModel'
import { expect,test } from 'bun:test'
import { defaultPreferences,normalizePreferences } from '../frontend/src/features/workspace/preferences'
import { canPause,canContinue,canRetry,remainingTime,transferSize } from '../frontend/src/features/workspace/transferModel'
import type { FileTask } from '../frontend/src/shared/bridge/files'

test('SCP keeps queued pause and explicit restart but does not promise seeking',()=>{expect(canPause({state:'running',protocol:'scp'} as FileTask)).toBe(false);expect(canPause({state:'queued',protocol:'scp'} as FileTask)).toBe(true);expect(canContinue({state:'failed',protocol:'scp',canResume:true} as FileTask)).toBe(true);expect(canRetry({state:'failed',protocol:'scp'} as FileTask)).toBe(true)})
test('transfer concurrency is bounded and whitelisted',()=>{expect(defaultPreferences.transferConcurrency).toBe(2);for(const n of [0,9,1.5,'2',null,Infinity])expect(normalizePreferences({transferConcurrency:n}).transferConcurrency).toBe(2);for(let n=1;n<=8;n++)expect(normalizePreferences({transferConcurrency:n}).transferConcurrency).toBe(n);expect(normalizePreferences({password:'secret',fileOffset:1024})).not.toHaveProperty('fileOffset')})
test('transfer actions distinguish continue from restarting a changed file',()=>{const task=(state:string,canResume=false)=>({state,canResume}) as FileTask;expect(canPause(task('running'))).toBe(true);expect(canPause(task('conflict',true))).toBe(false);expect(canContinue(task('paused',true))).toBe(true);expect(canContinue(task('failed',true))).toBe(true);expect(canContinue(task('failed',false))).toBe(false);expect(canRetry(task('failed'))).toBe(true);expect(canContinue(task('completed',true))).toBe(false);expect(canRetry(task('pausing'))).toBe(false)})
test('transfer labels handle unavailable ETA and byte boundaries',()=>{expect(remainingTime(-1)).toBe('');expect(remainingTime(Infinity)).toBe('');expect(remainingTime(23)).toBe('剩余 23 秒');expect(remainingTime(61)).toBe('剩余 2 分钟');expect(transferSize(1024)).toBe('1.0 KB');expect(transferSize(0)).toBe('0 B')})

test('closing either relay endpoint selects the same job and a group cancels it once', () => {
  const task = { id: 'relay', state: 'running', sessionId: 'A', sourceSession: 'A', destinationSession: 'B' } as FileTask
  const upload = { id: 'upload', state: 'paused', sessionId: 'B' } as FileTask
  const tasks = [task, upload, { ...task, id: 'finished', state: 'completed' }, { ...task, id: 'cancelled', state: 'cancelled' }]
  expect(transfersForClose(tasks, ['A'])).toEqual([task])
  expect(transfersForClose(tasks, ['B'])).toEqual([task, upload])
  expect(transfersForClose(tasks, ['C'])).toEqual([])
  expect(transfersForClose([...tasks, task], ['A', 'B'])).toEqual([task, upload])
})
