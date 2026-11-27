import {expect,test} from 'bun:test'
import {emptyHistory,visitDirectory,directoryStep,directoryCrumbs} from '../frontend/src/features/workspace/directoryHistory'

test('directory breadcrumbs keep exact paths and local grants relative',()=>{
 expect(directoryCrumbs('/srv/中文/a b')).toEqual([{label:'/',path:'/'},{label:'srv',path:'/srv'},{label:'中文',path:'/srv/中文'},{label:'a b',path:'/srv/中文/a b'}])
 expect(directoryCrumbs('/','project')).toEqual([{label:'project',path:'/'}])
 expect(directoryCrumbs('.')).toEqual([{label:'.',path:'.'}])
 expect(directoryCrumbs('/key#name','Windows folder').at(-1)).toEqual({label:'key#name',path:'/key#name'})
})
test('successful directory visits preserve branching and refresh does not duplicate history',()=>{
 let h=emptyHistory();h=visitDirectory(h,'/home');h=visitDirectory(h,'/home/files');expect(visitDirectory(h,'/home/files')).toBe(h)
 const back=directoryStep(h,-1)!;expect(back.path).toBe('/home');expect(h.index).toBe(1)
 h=visitDirectory(h,back.path,'history',back.index);expect(directoryStep(h,1)?.path).toBe('/home/files')
 h=visitDirectory(h,'/tmp');expect(h.paths).toEqual(['/home','/tmp']);expect(directoryStep(h,1)).toBeNull()
})
test('terminal follows coalesce bursts while explicit paths and independent histories remain intact',()=>{
 let remote=visitDirectory(emptyHistory(),'/home');const local=visitDirectory(emptyHistory(),'/')
 remote=visitDirectory(remote,'/a','follow',undefined,1000);remote=visitDirectory(remote,'/b','follow',undefined,1200);expect(remote.paths).toEqual(['/home','/b'])
 remote=visitDirectory(remote,'/explicit','navigate',undefined,1300);remote=visitDirectory(remote,'/next','follow',undefined,1400);expect(remote.paths).toEqual(['/home','/b','/explicit','/next']);expect(local.paths).toEqual(['/'])
 for(let i=0;i<1000;i++)remote=visitDirectory(remote,'/bounded/'+i);expect(remote.paths.length).toBe(64);expect(directoryStep(remote,1)).toBeNull()
})
