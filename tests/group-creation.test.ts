import {expect,test} from 'bun:test'
import {groupChoices,childGroupName,groupLabel,groupLeaf,groupParent,isComposingEnter} from '../frontend/src/features/connections/groupCreation'
import {previewChange} from '../frontend/src/features/workspace/savedWorkspace'
import type {SavedWorkspace} from '../frontend/src/features/workspace/savedWorkspace'
const initial:SavedWorkspace={revision:0,hosts:[],groups:['未分组','Production','Production/Database'],commands:[],commandGroups:['未分组']}
test('group input selects an exact match or offers one new top-level group before partial matches',()=>{
 expect(groupChoices(initial.groups,'Production')[0]!.name).toBe('Production')
 const options=groupChoices([...initial.groups,'Production','New York'],'New')
 expect(options[0]!.create).toBe(true);expect(options[0]!.value).toBe('create:New')
 expect(groupChoices(initial.groups,'Production › Database')[0]!.name).toBe('Production/Database')
 expect(groupChoices(initial.groups,'Database')[0]!.value).toBe('create:Database')
 expect(groupChoices(initial.groups,'')[0]!.name).toBe('未分组')
 const prefixed=groupChoices(['create:New'],'New')
 expect(new Set(prefixed.map(o=>o.value)).size).toBe(prefixed.length)
 expect(prefixed.map(o=>o.name)).toEqual(['New','create:New'])
 for(const invalid of ['a/b','a\\b','未分组','..','a'.repeat(33)])expect(groupChoices([],invalid).some(o=>o.create)).toBe(false)
})
test('child creation needs a valid existing parent and a leaf, permits names in different parents and bounds depth',()=>{
 expect(childGroupName('Production','Web',initial.groups)).toBe('Production/Web')
 expect(childGroupName('','Database',initial.groups)).toBe('Database')
 for(const [parent,name] of [['Missing','Web'],['Production','Database'],['Production','a/b'],['','未分组']])expect(()=>childGroupName(parent!,name!,initial.groups)).toThrow()
 const parent=Array.from({length:8},(_,i)=>'level'+i).join('/')
 expect(()=>childGroupName(parent,'child',[parent])).toThrow()
 expect(groupLeaf('Production/Database')).toBe('Database');expect(groupParent('Production/Database')).toBe('Production');expect(groupLabel('Production/Database')).toBe('Production › Database')
})
test('creating and renaming a child preserves hierarchy and canceling a connection draft leaves no empty group',()=>{
 const created=previewChange(initial,{kind:'addGroup',name:childGroupName('Production','Web',initial.groups)})
 const renamed=previewChange(created,{kind:'renameGroup',name:'Production',newName:'Services'})
 expect(renamed.groups).toContain('Services/Database');expect(renamed.groups).toContain('Services/Web')
 groupChoices(initial.groups,'Draft only');expect(initial.groups).not.toContain('Draft only')
})
test('Enter used by an input method is distinguished from group creation',()=>{
 expect(isComposingEnter({isComposing:true})).toBe(true);expect(isComposingEnter({keyCode:229})).toBe(true);expect(isComposingEnter({isComposing:false,keyCode:13})).toBe(false)
})

test('host group ordering starts in creation order and stores only a complete permutation', () => {
  let created = initial
  for (const name of ['Test 1', 'Test 2', 'Test 10'])
    created = previewChange(created, { kind: 'addGroup', name })
  expect(created.groups.slice(-3)).toEqual(['Test 1', 'Test 2', 'Test 10'])
  const order = ['未分组', 'Test 10', 'Production', 'Test 1', 'Production/Database', 'Test 2']
  const moved = previewChange(created, { kind: 'reorderGroups', groups: order })
  expect(moved.groups).toEqual(order)
  expect(previewChange(moved, { kind: 'addGroup', name: 'Later' }).groups).toEqual([...order, 'Later'])
  for (const invalid of [order.slice(1), ['Test 10', '未分组', ...order.slice(2)], [order[0]!, order[0]!, ...order.slice(2)]])
    expect(() => previewChange(created, { kind: 'reorderGroups', groups: invalid })).toThrow()
})
