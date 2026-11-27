import {expect,test} from 'bun:test'
import {initialHosts} from '../frontend/src/features/workspace/mockGateway'
import {filterHostList,reorderHostSubset,moveHostIDs,sidebarHostRows} from '../frontend/src/features/connections/hostOrdering'
import {hostSubtitle,hostWithColumnMoved,hostWithColumnVisibility,normalizeHostView,sortHostRows} from '../frontend/src/features/connections/hostColumns'
import {nextListSort} from '../frontend/src/shared/ui/listSort'
const hosts=['a','b','c','d','e'].map(id=>({...initialHosts[0]!,id,name:id,group:id==='c'?'Other':'Main',pinned:id==='b'||id==='d'}))
test('pin order is independent of ordinary order and unpinning returns to its saved position',()=>{
 const pins=reorderHostSubset(hosts,[],['d','b'],true)
 expect(pins.hosts).toEqual(hosts);expect(filterHostList(pins.hosts,'','all','all',pins.pinOrder).map(h=>h.id)).toEqual(['d','b','a','c','e'])
 const normal=reorderHostSubset(pins.hosts,pins.pinOrder,['e','a'],false)
 expect(normal.hosts.map(h=>h.id)).toEqual(['e','b','c','d','a']);expect(normal.pinOrder).toEqual(['d','b'])
 expect(filterHostList(normal.hosts.map(h=>h.id==='d'?{...h,pinned:false}:h),'','all','all',normal.pinOrder).map(h=>h.id)).toEqual(['b','e','c','d','a'])
 expect(hosts.map(h=>h.id)).toEqual(['a','b','c','d','e'])
})
test('buttons and drag destinations use the same boundary-safe move calculation',()=>{
 expect(moveHostIDs(['a','c','e'],'c','first')).toEqual(moveHostIDs(['a','c','e'],'c',0))
 expect(moveHostIDs(['a','c','e'],'c','last')).toEqual(moveHostIDs(['a','c','e'],'c',2))
 expect(moveHostIDs(['a','c','e'],'a','up')).toEqual(['a','c','e'])
 expect(moveHostIDs(['a','c','e'],'e','down')).toEqual(['a','c','e'])
 for(const ids of [[],['a','a'],['a','b'],['missing']])expect(()=>reorderHostSubset(hosts,[],ids,false)).toThrow()
})
test('group moves preserve hidden slots and sidebar follows the independent pin order',()=>{
 const visible=filterHostList(hosts,'','group:Main').filter(h=>!h.pinned).map(h=>h.id)
 const moved=reorderHostSubset(hosts,['d','b'],moveHostIDs(visible,'e','first'),false)
 expect(moved.hosts[2]!.id).toBe('c')
 const rows=sidebarHostRows(moved.hosts,moved.pinOrder).filter(h=>!('count'in h))
 expect(rows.slice(0,2).map(h=>h.id)).toEqual(['d','b']);expect(rows).toHaveLength(5)
})

test('host column preferences preserve a valid order and clear a hidden sort', () => {
  const defaults = normalizeHostView(null)
  expect(defaults.columns.filter(column => column.visible).map(column => column.key)).toEqual(['group'])
  const view = hostWithColumnVisibility(defaults, 'port', true)
  const moved = hostWithColumnMoved(view, 'port', 0)
  const saved = normalizeHostView({
    columns: [{ key: 'unknown', visible: true }, ...moved.columns, { key: 'port', visible: false }],
    sort: nextListSort(null, 'port'),
  })
  expect(saved.columns.filter(column => column.visible).map(column => column.key)).toEqual(['port', 'group'])
  expect(saved.sort).toEqual({ key: 'port', direction: 'ascending' })
  expect(hostWithColumnVisibility(saved, 'port', false).sort).toBeNull()
  expect(normalizeHostView({ columns: defaults.columns, sort: saved.sort }).sort).toBeNull()
})

test('table sorting keeps pins above ordinary rows and returns to untouched manual order', () => {
  const manual = filterHostList(hosts, '', 'all', 'all', ['d','b'])
  const collator = new Intl.Collator('en', { numeric: true })
  const value = (host: typeof hosts[number]) => host.id === 'd' ? 200 : host.id === 'b' ? 2 : host.id === 'a' ? 10 : undefined
  const ascending = nextListSort(null, 'port')
  const descending = nextListSort(ascending, 'port')
  expect(sortHostRows(manual, ascending, collator, value).map(host => host.id)).toEqual(['b','d','a','c','e'])
  expect(sortHostRows(manual, descending, collator, value).map(host => host.id)).toEqual(['d','b','a','c','e'])
  expect(sortHostRows(manual, nextListSort(descending, 'port'), collator, value)).toBe(manual)
  expect(manual.map(host => host.id)).toEqual(['d','b','a','c','e'])
})

test('host subtitle leaves separately displayed fields out', () => {
  const host = {...hosts[0]!, address: '2001:db8::1', username: 'ops', port: 2222}
  expect(hostSubtitle(host, 'Debian 12', [])).toBe('Debian 12 · ops@[2001:db8::1]:2222')
  expect(hostSubtitle(host, 'Debian 12', ['username', 'port', 'system'])).toBe('2001:db8::1')
  expect(hostSubtitle(host, 'Debian 12', ['address', 'username', 'port', 'system'])).toBe('')
})
