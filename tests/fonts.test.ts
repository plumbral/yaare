import {expect,test} from 'bun:test'
import {createLocalFontCatalog,localFonts} from '../frontend/src/shared/fonts/localFonts'
import {isLocalFont,localFontName} from '../frontend/src/shared/fonts/localFontId'
import {uiFonts,terminalFonts,fallbackFonts,uiFontStack,terminalFontStack,normalizeUIFont,normalizeTerminalFont,normalizeFallbackFont,applyUIFont,fontAvailable,prepareTerminalFonts,prepareTerminalPrimary} from '../frontend/src/shared/fonts/fonts'
import {detectFontPlatform,systemMonoFamilies,systemMonoName} from '../frontend/src/shared/fonts/platformFonts'
import {normalizePreferences} from '../frontend/src/features/workspace/preferences'
import {readFileSync,readdirSync} from 'node:fs'
import {createHash} from 'node:crypto'
import manifest from '../scripts/fonts-manifest.json'

test('offline font assets match locked hashes and keep the complete default set below 6 MiB',()=>{
  let bytes=0
  for(const [name,info] of Object.entries(manifest.outputs)){
    const file=readFileSync(new URL('../frontend/src/shared/fonts/assets/'+name+'.woff2',import.meta.url))
    expect(file.subarray(0,4).toString()).toBe('wOF2')
    expect(createHash('sha256').update(file).digest('hex')).toBe(info.sha256)
    expect(file.length).toBe(info.bytes);bytes+=file.length
  }
  expect(bytes).toBeLessThan(6*1024*1024)
  expect(readdirSync(new URL('../frontend/src/shared/fonts/assets/',import.meta.url)).sort()).toEqual(Object.keys(manifest.outputs).map(name=>name+'.woff2').sort())
  expect(manifest.outputs['maple-mono-cn'].characters).toBe(22731)
  for(const name of ['inter','google-sans-code','maple-mono'])expect(readFileSync(new URL('../resources/licenses/'+name+'-OFL.txt',import.meta.url),'utf8')).toContain('SIL OPEN FONT LICENSE')
})

test('current font choices reject retired IDs and retain other saved preferences',()=>{
  expect(normalizePreferences({})).toMatchObject({uiFont:'Inter',fontFamily:'Google Sans Code',fallbackFont:'Maple Mono CN'})
  expect(uiFonts).toEqual(['Inter','system'])
  expect(terminalFonts).toEqual(['Google Sans Code','system-mono'])
  expect(fallbackFonts).toEqual(['Maple Mono CN','system'])
  for(const fontFamily of ['system-mono','Google Sans Code'])expect(normalizePreferences({fontFamily})).toMatchObject({fontFamily,uiFont:'Inter',fallbackFont:'Maple Mono CN'})
  for(const fontFamily of ['Consolas','monospace'])expect(normalizePreferences({fontFamily})).toMatchObject({fontFamily:'Google Sans Code',uiFont:'Inter',fallbackFont:'Maple Mono CN'})
  for(const uiFont of ['MiSans','HarmonyOS Sans','Noto Sans CJK'])expect(normalizePreferences({uiFont,language:'ja',fontSize:18,directorySync:true})).toMatchObject({uiFont:'Inter',language:'ja',fontSize:18,directorySync:true})
  for(const fontFamily of ['JetBrains Mono','Cascadia Code','Maple Mono CN'])expect(normalizeTerminalFont(fontFamily)).toBe('Google Sans Code')
  for(const fallbackFont of ['Sarasa Term SC','Sarasa Term TC','Sarasa Term J'])expect(normalizeFallbackFont(fallbackFont)).toBe('Maple Mono CN')
  expect(normalizePreferences({uiFont:'system',fallbackFont:'system'})).toMatchObject({uiFont:'system',fallbackFont:'system'})
  expect(normalizeUIFont('url(https://example.invalid/font)')).toBe('Inter')
  expect(normalizeTerminalFont('arbitrary CSS')).toBe('Google Sans Code')
  expect(normalizeFallbackFont({family:'secret'})).toBe('Maple Mono CN')
})
test('fallback ordering preserves the CJK choice and interface regional forms',()=>{
  expect(terminalFontStack('Google Sans Code','Maple Mono CN')).toStartWith('"Google Sans Code", "Yaare Maple Mono CN"')
  expect(terminalFontStack('Google Sans Code','system')).not.toContain('Yaare Maple')
  expect(uiFontStack('Inter','ja')).toContain('system-ui')
  expect(uiFontStack('Inter','ja')).toContain('Yu Gothic UI')
  expect(uiFontStack('Inter','zh-Hant')).toContain('Microsoft JhengHei UI')
  expect(uiFontStack('Inter','zh-Hans')).toContain('Microsoft YaHei UI')
})

test('system monospace adapts to the viewing device as one current preference',()=>{
  for(const value of ['Win32','Windows','Windows NT'])expect(detectFontPlatform(value)).toBe('windows')
  for(const value of ['MacIntel','macOS','Darwin'])expect(detectFontPlatform(value)).toBe('macos')
  for(const value of ['Linux x86_64','X11','CrOS'])expect(detectFontPlatform(value)).toBe('linux')
  expect(detectFontPlatform('unknown')).toBe('other')
  expect(systemMonoName('windows')).toBe('Consolas')
  expect(systemMonoName('macos')).toBe('Menlo')
  expect(systemMonoFamilies('linux')[0]).toBe('DejaVu Sans Mono')
  expect(systemMonoFamilies('other')).toEqual([])
})

test('removed font choices cannot trigger local font probing or loading',async()=>{
  expect(await fontAvailable('MiSans')).toBe(false)
  expect(await fontAvailable('Sarasa Term TC')).toBe(false)
  expect(await fontAvailable('system-mono')).toBe(true)
  expect(await fontAvailable('system')).toBe(true)
})
test('local family preferences survive normalization and escape CSS',()=>{
  const raw={uiFont:'local:Microsoft YaHei UI',fontFamily:'local:Consolas',fallbackFont:'local:等距更紗黑體 TC',language:'ja',fontSize:18}
  expect(normalizePreferences(raw)).toMatchObject(raw)
  expect(uiFontStack('local:My "Quoted" \\ Font','en')).toStartWith('"My \\"Quoted\\" \\\\ Font", ')
  expect(terminalFontStack('local:JetBrains Mono','local:MS Gothic')).toStartWith('"JetBrains Mono", "MS Gothic"')
  for(const value of ['local:', 'local: Arial', 'local:Arial ', 'local:A\nB', 'local:A\u0085B', 'local:'+ '字'.repeat(129)])expect(isLocalFont(value)).toBe(false)
  expect(isLocalFont('local:'+'字'.repeat(128))).toBe(true)
  expect(localFontName('local:MS Gothic')).toBe('MS Gothic')
})
test('font enumeration is lazy, shared, sorted and deduplicated by family, and never reads a browser server',async()=>{
  let reads=0,enabled=false
  const catalog=createLocalFontCatalog(async()=>{reads++;return [{name:'zeta',monospaced:false},{name:'Arial',monospaced:false},{name:'arial',monospaced:true},{name:'@Vertical',monospaced:true},{name:'bad\nname',monospaced:false}]},()=>enabled)
  expect(reads).toBe(0);expect(await catalog.list()).toEqual([]);expect(reads).toBe(0)
  enabled=true
  const first=catalog.list(),second=catalog.list()
  expect(first).toBe(second)
  expect(await first).toEqual([{name:'Arial',monospaced:true},{name:'zeta',monospaced:false}])
  await catalog.list();expect(reads).toBe(1)
})
test('a failed enumeration can retry and does not poison the process cache',async()=>{
  let reads=0
  const catalog=createLocalFontCatalog(async()=>{if(++reads===1)throw Error('test');return [{name:'Mono',monospaced:true}]},()=>true)
  await expect(catalog.list()).rejects.toThrow('test')
  expect(await catalog.list()).toEqual([{name:'Mono',monospaced:true}]);expect(reads).toBe(2)
})
test('the initial terminal can measure its primary font while the CJK fallback remains pending', async () => {
  const previous = Object.getOwnPropertyDescriptor(globalThis, 'document')
  const requested: string[] = []
  let resolveFallback!: (faces: unknown[]) => void
  const fallback = new Promise<unknown[]>(resolve => { resolveFallback = resolve })
  Object.defineProperty(globalThis, 'document', { configurable: true, value: { fonts: { load: (font: string) => {
    requested.push(font)
    return font.includes('Maple Mono CN') ? fallback : Promise.resolve([{}])
  } } } })
  try {
    const primary = await prepareTerminalPrimary('Google Sans Code')
    expect(requested).toEqual(['400 14px "Google Sans Code"'])
    expect(terminalFontStack(primary, 'system')).not.toContain('Maple Mono CN')
    let installed = false
    const full = prepareTerminalFonts(primary, 'Maple Mono CN').then(family => { installed = true; return family })
    expect(requested).toContain('400 14px "Yaare Maple Mono CN"')
    expect(installed).toBe(false)
    resolveFallback([{}])
    expect(await full).toBe(terminalFontStack(primary, 'Maple Mono CN'))
  } finally {
    resolveFallback([])
    if (previous) Object.defineProperty(globalThis, 'document', previous)
    else Reflect.deleteProperty(globalThis, 'document')
  }
})
test('missing local fonts use bundled defaults without discarding the desired portable preference',async()=>{
  const saved={uiFont:'local:Absent UI',fontFamily:'local:Absent Mono',fallbackFont:'local:Absent CJK'}
  expect(normalizePreferences(saved)).toMatchObject(saved)
  expect(await fontAvailable('local:Absent Mono')).toBe(false)
  expect(await prepareTerminalFonts('local:Absent Mono','local:Absent CJK')).toStartWith('"Google Sans Code", "Yaare Maple Mono CN"')
})
test('local choices verify WebView access and a rejected local face is not reported as usable',async()=>{
  const oldList=localFonts.list,oldFace=globalThis.FontFace
  const sources:string[]=[]
  localFonts.list=async()=>[{name:'Test Local Mono',monospaced:true},{name:'Blocked Local',monospaced:false}]
  class LocalFace {constructor(_family:string,public source:string){sources.push(source)}async load(){if(this.source.includes('Blocked'))throw Error('unavailable');return this}}
  Object.defineProperty(globalThis,'FontFace',{configurable:true,value:LocalFace})
  try {
    expect(await fontAvailable('local:Test Local Mono')).toBe(true)
    expect(await fontAvailable('local:Test Local Mono')).toBe(true)
    expect(sources).toEqual(['local("Test Local Mono")'])
    expect(await fontAvailable('local:Blocked Local')).toBe(false)
    expect(await fontAvailable('local:Not Installed')).toBe(false)
  }finally{localFonts.list=oldList;Object.defineProperty(globalThis,'FontFace',{configurable:true,value:oldFace})}
})
test('a slow earlier font load cannot overwrite the latest interface selection',async()=>{
  const old=globalThis.document
  let finish:(value:unknown[])=>void=()=>{}
  const styles=new Map<string,string>()
  Object.defineProperty(globalThis,'document',{configurable:true,value:{fonts:{load:()=>new Promise(resolve=>{finish=resolve})},documentElement:{style:{setProperty:(k:string,v:string)=>styles.set(k,v)}}}})
  try {
    const earlier=applyUIFont('Inter','en')
    await applyUIFont('system','en')
    finish([{}]);await earlier
    expect(styles.get('--yaare-ui-font')).toStartWith('system-ui')
  }finally{Object.defineProperty(globalThis,'document',{configurable:true,value:old})}
})
