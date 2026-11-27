import {expect,test} from 'bun:test'
import {
  fileEntryIconCategory,
  flattenTree,
  parseFileDrag,
  selectTree,
  selectionGroups,
  summarizeRootDirectory,
  summarizeSelection,
  type FileNode,
} from '../frontend/src/features/workspace/fileTreeModel'
import {
  clampFileColumnWidth,
  clampSftpSplitRatio,
  nextFileSort,
  normalizeColumnWidths,
  normalizeFileSort,
  normalizeSideViewPreferences,
  resolveFileColumnWidths,
  visibleFileColumnIds,
} from '../frontend/src/features/workspace/fileColumns'
const entry=(name:string,directory=false,extra?:Partial<{link:boolean;size:number;modified:number;mode:string;permissions:string}>)=>({name,directory,link:false,size:1,modified:1,mode:'',permissions:'644',...extra})
test('tree expansion exposes only loaded children, aligns hierarchy and excludes unsafe names',()=>{
  const dirs=new Map([['/srv',[entry('z.txt'),entry('app',true),entry('.env'),entry('..',true),entry('bad/name')]],['/srv/app',[entry('b.txt'),entry('a.txt')]]])
  expect(flattenTree('/srv',dirs,new Set(),false,'','name').map(n=>n.path)).toEqual(['/srv/app','/srv/z.txt'])
  const expanded=flattenTree('/srv',dirs,new Set(['/srv/app']),false,'','name')
  expect(expanded.map(n=>[n.path,n.depth,n.position,n.siblings])).toEqual([['/srv/app',0,1,2],['/srv/app/a.txt',1,1,2],['/srv/app/b.txt',1,2,2],['/srv/z.txt',0,2,2]])
  expect(flattenTree('/srv',dirs,new Set(['/srv/app']),false,'a.txt','name').map(n=>n.path)).toEqual(['/srv/app','/srv/app/a.txt'])
})
test('plain click replaces selection, modifiers extend visible ranges and control toggles',()=>{
  const ids=['dir','dir/a','dir/b','file'];let state=selectTree(ids,new Set(),'dir/a','')
  state=selectTree(ids,state.selected,'file',state.anchor,true);expect([...state.selected]).toEqual(['dir/a','file'])
  state=selectTree(ids,state.selected,'dir/a',state.anchor,true);expect([...state.selected]).toEqual(['file'])
  state=selectTree(ids,state.selected,'dir/b','dir',false,true);expect([...state.selected]).toEqual(['dir','dir/a','dir/b'])
  state=selectTree(ids,state.selected,'file',state.anchor);expect([...state.selected]).toEqual(['file'])
  const tooMany=selectTree(Array.from({length:1000},(_,i)=>String(i)),new Set(['0']),'999','0',false,true);expect(tooMany.limited).toBe(true);expect([...tooMany.selected]).toEqual(['0'])
})
test('batch operations distinguish same-named files and deduplicate selected directory descendants',()=>{
  const node=(path:string,directory=false):FileNode=>({path,parent:path.slice(0,path.lastIndexOf('/'))||'/',entry:entry(path.split('/').pop()!,directory),depth:0,position:1,siblings:1})
  expect(selectionGroups([node('/srv/a/config'),node('/srv/b/config')])).toEqual([{path:'/srv/a',names:['config']},{path:'/srv/b',names:['config']}])
  expect(selectionGroups([node('/srv/a',true),node('/srv/a/config'),node('/srv/a/nested',true),node('/srv/b/config')])).toEqual([{path:'/srv',names:['a']},{path:'/srv/b',names:['config']}])
})

test('file drags preserve each source directory and reject a malformed batch before queuing it', () => {
  const remote = { side: 'remote', path: '/srv', sessionId: 'pane', attempt: 1, token: '' }
  const local = { side: 'local', path: '/', sessionId: '', attempt: 0, token: 'local-folder' }
  const first = { source: remote, names: ['config', '配置.json'] }
  const second = { source: local, names: ['config'] }
  const names = Array.from({ length: 255 }, (_, index) => String(index))
  for (const groups of [[first], [first, second], [{ source: remote, names }, second]]) {
    expect(parseFileDrag(JSON.stringify({ groups }))).toEqual(groups)
  }
  for (const payload of [
    first, null, { groups: [] }, { groups: Array(257).fill(first) },
    { groups: [{ source: local, names }, first] },
    { groups: [first, null] }, { groups: [first, { names: ['config'] }] },
    { groups: [{ source: remote, names: [] }] },
    { groups: [{ source: local, names: ['../config'] }] },
    { groups: [{ source: { ...remote, side: 'drop' }, names: ['config'] }] },
  ]) {
    expect(parseFileDrag(JSON.stringify(payload))).toBeNull()
  }
  expect(parseFileDrag('{')).toBeNull()
})

test('file tree sorting supports all columns, ascending/descending toggles, and natural ordering',()=>{
  const dirs=new Map([['/srv',[
    entry('dir-b',true,{modified:10}),
    entry('dir-a',true,{modified:30}),
    entry('log10.txt',false,{size:500,modified:100}),
    entry('log2.txt',false,{size:100,modified:300}),
    entry('app.go',false,{size:300,modified:200}),
    entry('archive.tar.gz',false,{size:900,modified:50}),
  ]]])

  expect(flattenTree('/srv',dirs,new Set(),false,'',{key:'name',direction:'ascending'},'en').map(n=>n.entry.name))
    .toEqual(['dir-a','dir-b','app.go','archive.tar.gz','log2.txt','log10.txt'])

  expect(flattenTree('/srv',dirs,new Set(),false,'',{key:'name',direction:'descending'},'en').map(n=>n.entry.name))
    .toEqual(['dir-b','dir-a','log10.txt','log2.txt','archive.tar.gz','app.go'])

  expect(flattenTree('/srv',dirs,new Set(),false,'',{key:'size',direction:'descending'},'en').map(n=>n.entry.name))
    .toEqual(['dir-a','dir-b','archive.tar.gz','log10.txt','app.go','log2.txt'])
  expect(flattenTree('/srv',dirs,new Set(),false,'',{key:'size',direction:'ascending'},'en').map(n=>n.entry.name))
    .toEqual(['dir-a','dir-b','log2.txt','app.go','log10.txt','archive.tar.gz'])

  expect(flattenTree('/srv',dirs,new Set(),false,'',{key:'modified',direction:'descending'},'en').map(n=>n.entry.name))
    .toEqual(['dir-a','dir-b','log2.txt','app.go','log10.txt','archive.tar.gz'])

  expect(flattenTree('/srv',dirs,new Set(),false,'',{key:'type',direction:'ascending'},'en').map(n=>n.entry.name))
    .toEqual(['dir-a','dir-b','app.go','archive.tar.gz','log2.txt','log10.txt'])

  expect(nextFileSort({key:'name',direction:'ascending'},'name')).toEqual({key:'name',direction:'descending'})
  expect(nextFileSort({key:'name',direction:'ascending'},'modified')).toEqual({key:'modified',direction:'descending'})
  expect(nextFileSort({key:'modified',direction:'descending'},'size')).toEqual({key:'size',direction:'descending'})
  expect(nextFileSort({key:'size',direction:'descending'},'type')).toEqual({key:'type',direction:'ascending'})
  expect(normalizeFileSort('size')).toEqual({key:'size',direction:'descending'})
})

test('resizable file columns fill available space by default and enable horizontal overflow when widened',()=>{
  const cols=visibleFileColumnIds(560,{showPermissions:false,showOwner:false})
  expect(cols).toEqual(['name','modified','size','type'])

  const initial=resolveFileColumnWidths(cols,560,{})
  expect(initial.horizontalOverflow).toBe(false)
  expect(initial.totalTrackWidth).toBe(560)
  expect(initial.widths.name).toBeGreaterThanOrEqual(140)

  const narrowed=resolveFileColumnWidths(cols,560,{modified:96})
  expect(narrowed.horizontalOverflow).toBe(false)
  expect(narrowed.widths.name).toBeGreaterThan(initial.widths.name)

  const widened=resolveFileColumnWidths(cols,560,{name:520})
  expect(widened.horizontalOverflow).toBe(true)
  expect(widened.totalTrackWidth).toBeGreaterThan(560)
  expect(widened.widths.name).toBe(520)

  expect(visibleFileColumnIds(480,{showPermissions:true,showOwner:true})).toContain('permissions')
  expect(visibleFileColumnIds(480,{showPermissions:true,showOwner:true})).toContain('owner')

  expect(clampFileColumnWidth('name',20)).toBe(140)
  expect(clampFileColumnWidth('size',9999)).toBe(180)
  expect(normalizeColumnWidths({name:9999,size:10,bad:'x'})).toEqual({name:1200,size:56})
  expect(clampSftpSplitRatio(0.1,1000)).toBe(0.26)
  expect(clampSftpSplitRatio(0.9,1000)).toBe(0.74)
  expect(normalizeSideViewPreferences({wrapNames:true,showPermissions:true,sort:{key:'type',direction:'descending'}}))
    .toEqual({wrapNames:true,showHidden:false,showPermissions:true,showOwner:false,sort:{key:'type',direction:'descending'},columnWidths:{}})
})

test('directory and selection summaries accurately separate hidden dotfiles and file bytes',()=>{
  const entries=[
    entry('.bashrc',false,{size:120}),
    entry('.ssh',true,{size:4096}),
    entry('.profile',false,{size:80}),
    entry('..',true),
    entry('bad/entry',false),
  ]
  expect(summarizeRootDirectory(entries,false)).toEqual({total:3,visible:0,hidden:3})
  expect(summarizeRootDirectory(entries,true)).toEqual({total:3,visible:3,hidden:3})

  const nodes:FileNode[]=[
    {path:'/srv/app',parent:'/srv',entry:entry('app',true,{size:4096}),depth:0,position:1,siblings:3},
    {path:'/srv/a.bin',parent:'/srv',entry:entry('a.bin',false,{size:1024}),depth:0,position:2,siblings:3},
    {path:'/srv/b.bin',parent:'/srv',entry:entry('b.bin',false,{size:2048}),depth:0,position:3,siblings:3},
    {path:'/srv/link',parent:'/srv',entry:entry('link',false,{link:true,size:99}),depth:0,position:4,siblings:4},
  ]
  expect(summarizeSelection(nodes)).toEqual({count:4,fileCount:2,totalBytes:3072})
  expect(summarizeSelection([nodes[0]!])).toEqual({count:1,fileCount:0,totalBytes:0})
})

test('file icon categories distinguish symlinks, folders, archives, media, code and text',()=>{
  expect(fileEntryIconCategory(entry('latest',false,{link:true}))).toBe('link')
  expect(fileEntryIconCategory(entry('logs',true))).toBe('folder')
  expect(fileEntryIconCategory(entry('backup.tar.gz'))).toBe('archive')
  expect(fileEntryIconCategory(entry('screenshot.png'))).toBe('image')
  expect(fileEntryIconCategory(entry('demo.mp4'))).toBe('video')
  expect(fileEntryIconCategory(entry('alert.wav'))).toBe('audio')
  expect(fileEntryIconCategory(entry('Dockerfile'))).toBe('code')
  expect(fileEntryIconCategory(entry('server.ts'))).toBe('code')
  expect(fileEntryIconCategory(entry('notes.log'))).toBe('text')
  expect(fileEntryIconCategory(entry('firmware.bin'))).toBe('file')
})
