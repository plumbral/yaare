import { localizeMessage } from '../../shared/i18n/messages'
import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import {useEffect,useRef,useState} from 'react'
import {Stack,StackItem} from '@astryxdesign/core/Stack'
import {Text,Heading} from '@astryxdesign/core/Text'
import {Button} from '@astryxdesign/core/Button'
import {TextInput} from '@astryxdesign/core/TextInput'
import {Selector} from '@astryxdesign/core/Selector'
import {Switch} from '@astryxdesign/core/Switch'
import {InputGroup} from '@astryxdesign/core/InputGroup'
import {IconButton} from '@astryxdesign/core/IconButton'
import {Icon} from '@astryxdesign/core/Icon'
import {EyeIcon,EyeSlashIcon} from '@heroicons/react/24/outline'
import EditorDialog from '../../shared/ui/EditorDialog'
import {Dialog} from '../../shared/ui/AppDialog'
import {DialogHeader} from '@astryxdesign/core/Dialog'
import {desktopGateway} from '../../shared/bridge/desktop'
import {desktopAvailable,setWindowFullscreen} from '../../shared/bridge/application'
import {watchElementFullscreen} from './elementFullscreen'
import {createVNCChannel} from './vncChannel'
import type RFB from '@novnc/novnc'

const errorText=(e:unknown)=>e instanceof Error?e.message:tr("远程桌面连接失败，请检查服务后重新连接。")
export default function RemoteDesktopWorkspace({sessionId,attempt,connected,active,onClose,gateway=desktopGateway}:{sessionId:string;attempt:number;connected:boolean;active:boolean;onClose:()=>void;gateway?:typeof desktopGateway}){
  useLocale()

 const browserViewer = import.meta.env.MODE === 'browser'
 const [kind,setKind]=useState('vnc'),[host,setHost]=useState('127.0.0.1'),[port,setPort]=useState('5900'),[user,setUser]=useState(''),[phase,setPhase]=useState<'idle'|'opening'|'connected'>('idle'),[error,setError]=useState(''),[notice,setNotice]=useState(''),[readOnly,setReadOnly]=useState(false),[auth,setAuth]=useState<string[]|null>(null),[password,setPassword]=useState(''),[authUser,setAuthUser]=useState(''),[authTarget,setAuthTarget]=useState(''),[showSecret,setShowSecret]=useState(false)
 const canvas=useRef<HTMLElement>(null),root=useRef<HTMLElement>(null),rfb=useRef<RFB|null>(null),stop=useRef<(()=>void)|null>(null),raw=useRef<ReturnType<typeof createVNCChannel>|null>(null),epoch=useRef(0),mounted=useRef(true)
 const currentReadOnly=useRef(readOnly);currentReadOnly.current=readOnly
 const [cookie,setCookie]=useState(''),[command,setCommand]=useState('xterm'),[confirmX11,setConfirmX11]=useState(false),[fullscreen,setFullscreen]=useState(false)
 useEffect(()=>{const element=root.current;if(!element)return;return watchElementFullscreen(element,!browserViewer && desktopAvailable()?setWindowFullscreen:undefined,setFullscreen,()=>setError(tr("未能切换全屏，请在当前窗口中查看。")))},[])
 function disconnect(){if(document.fullscreenElement===root.current)void document.exitFullscreen().catch(()=>{});epoch.current++;const previous=rfb.current;rfb.current=null;previous?.disconnect();raw.current?.end();raw.current=null;stop.current?.();stop.current=null;setPhase('idle');setAuth(null);setPassword('');setAuthUser('');setAuthTarget('');setShowSecret(false);setCookie('');setConfirmX11(false)}
 useEffect(()=>{mounted.current=true;return()=>{mounted.current=false;epoch.current++;rfb.current?.disconnect();raw.current?.end();stop.current?.()}},[])
 useEffect(()=>{disconnect();setError('');setNotice('')},[sessionId,attempt,connected])
 useEffect(()=>{if(!active){setConfirmX11(false);setCookie('');if(document.fullscreenElement===root.current)void document.exitFullscreen().catch(()=>{});if(kind==='vnc'&&phase!=='idle'){disconnect();setNotice(tr("已暂停画面并释放 VNC 通道，返回后可重新连接。"))}}},[active])
 useEffect(()=>{const changed=()=>{if(document.visibilityState==='hidden'&&kind==='vnc'&&phase!=='idle'){disconnect();setNotice(tr("已暂停画面并释放 VNC 通道，返回后可重新连接。"))}};document.addEventListener('visibilitychange',changed);return()=>document.removeEventListener('visibilitychange',changed)},[kind,phase])
 useEffect(()=>{if(rfb.current)rfb.current.viewOnly=readOnly},[readOnly])
 async function open(confirmed=false){if(phase!=='idle'||!connected||!gateway.available())return;if(kind==='x11'&&!confirmed){setConfirmX11(true);return};const privateCookie=cookie;disconnect();const token=epoch.current;setPhase('opening');setError('');setNotice('');const target={host:host.trim(),port:Number(port)}
  const fail=(cause:unknown)=>{if(token!==epoch.current||!mounted.current)return;disconnect();setError(errorText(cause))}
  const done=()=>{if(token!==epoch.current||!mounted.current)return;disconnect();setNotice(tr("远程桌面通道已结束，请在需要时重新打开。"))}
  if(kind==='x11'){const release=gateway.x11(sessionId,attempt,{local:target,cookie:privateCookie,command,confirmed:true},state=>{if(token!==epoch.current||!mounted.current)return;if(state==='ready'){setPhase('connected');setNotice(tr("X11 转发已开启，图形程序显示在本机 X 服务器。"))}else if(state==='attached')setNotice(tr("已连接本机 X 显示。"));else if(state==='authentication-failed')setError(tr("本机 X 服务器拒绝认证，请检查 Cookie 和访问控制。"));else if(state==='local-unavailable')setError(tr("无法连接本机 X 服务器，请检查地址、端口及启动状态。"))},fail,done);if(token===epoch.current)stop.current=release;else release();return}
  if(kind==='rdp'){const release=gateway.rdp(sessionId,attempt,target,user,()=>{if(token===epoch.current&&mounted.current){setPhase('connected');setNotice(tr("本机通道已开启。请在 Windows 远程桌面中完成认证并核对服务器证书。"))}},fail,done);if(token===epoch.current)stop.current=release;else release();return}
  try{
   const [{default:RFB},{initLogging}]=await Promise.all([import('@novnc/novnc'),import('@yaare/novnc-logging')]);if(token!==epoch.current||!mounted.current||!canvas.current)return
   initLogging('none')
   const transport=createVNCChannel(gateway.write,()=>stop.current?.(),fail);raw.current=transport
   const viewer=new RFB(canvas.current,transport.channel,{shared:true});rfb.current=viewer;viewer.scaleViewport=true;viewer.resizeSession=false;viewer.viewOnly=currentReadOnly.current;viewer.qualityLevel=4;viewer.compressionLevel=4
   viewer.addEventListener('connect',()=>{if(token===epoch.current&&mounted.current){setPhase('connected');setAuth(null);setPassword('');setAuthUser('');setAuthTarget('');setShowSecret(false)}})
   viewer.addEventListener('disconnect',done)
   viewer.addEventListener('securityfailure',()=>fail(new Error(tr("VNC 认证失败，请检查账户或密码后重新连接。"))))
   viewer.addEventListener('credentialsrequired',event=>{if(token!==epoch.current||!mounted.current)return;const types=(event as CustomEvent<{types:string[]}>).detail.types;if(types.some(type=>!['username','password','target'].includes(type))){fail(new Error(tr("VNC 请求了暂不支持的认证方式。")));return};setAuth(types);setPassword('')})
   const release=gateway.vnc(sessionId,attempt,target,packet=>{if(token===epoch.current)transport.packet(packet)},fail,done);if(token===epoch.current)stop.current=release;else release()
  }catch(e){fail(e)}
 }
 return <Stack ref={root} role="region" aria-label={tr("远程桌面")} hidden={!active} inert={!active} style={{display:active?'flex':'none',height:'100%',minHeight:0,minWidth:0,background:'var(--color-background-body)'}}>
  <Stack padding={3} gap={2} style={{flexShrink:0}}><Stack direction="horizontal" align="center" gap={2} wrap="wrap"><StackItem size="fill"><Heading level={4} accessibilityLevel={2}>{tr("远程桌面")}</Heading></StackItem><Button label={tr("返回终端")} variant="ghost" onClick={onClose}/>{phase!=='idle'&&<Button label={kind==='rdp'?tr("停止 RDP 通道"):kind==='x11'?tr("停止 X11"):tr("断开 VNC")} variant="secondary" onClick={disconnect}/>}</Stack>
   <Text type="supporting" color="secondary" role="status">{phase==='opening'?tr("正在连接…"):notice||(phase==='connected'?kind.toUpperCase()+' · '+host+':'+port:browserViewer?tr("Web 版可通过 SSH 连接 VNC。RDP 和 X11 尚未支持。"):tr("通过 SSH 连接 VNC、RDP，或启动 X11 图形程序。"))}</Text>{error&&<Text role="alert" type="supporting">{localizeMessage(error)}</Text>}
  </Stack>
  {phase==='idle'&&<Stack padding={4} style={{overflow:'auto',minHeight:0}}><form onSubmit={e=>{e.preventDefault();void open()}} style={{display:'flex',flexDirection:'column',gap:'var(--spacing-3)',width:'100%',maxWidth:'calc(var(--spacing-10) * 14)',marginInline:'auto'}}>
   {!browserViewer&&<Selector label={tr("连接方式")} value={kind} options={[{label:tr("VNC · 内嵌显示"),value:'vnc'},{label:tr("RDP · Windows 客户端"),value:'rdp'},{label:tr("X11 · 本机 X 服务器"),value:'x11'}]} onChange={value=>{if(value==='x11'||kind==='x11')setHost('127.0.0.1');setKind(value);setPort(value==='vnc'?'5900':value==='rdp'?'3389':'6000');setCookie('');setShowSecret(false);setError('');setNotice('')}}/>}
   <Stack direction="horizontal" gap={3} wrap="wrap"><StackItem size="fill" style={{minWidth:'calc(var(--spacing-10) * 4)'}}><TextInput label={kind==='x11'?tr("本机 X 服务器地址"):tr("服务器侧地址")} value={host} onChange={setHost} autoComplete="off"/></StackItem><TextInput label={tr("端口")} value={port} onChange={setPort} inputMode="numeric" width="calc(var(--spacing-10) * 2.5)"/></Stack>
   {kind==='rdp'&&<TextInput label={tr("Windows 用户名（可选）")} value={user} onChange={setUser} autoComplete="off"/>}
   {kind==='x11'&&<><TextInput label={tr("远程图形命令")} value={command} onChange={setCommand} autoComplete="off"/><InputGroup label={tr("本机 X11 Cookie（可选）")} size="md"><TextInput label={tr("X11 认证内容")} type={showSecret?'text':'password'} value={cookie} onChange={setCookie} autoComplete="off" hasAutoCorrect={false} hasSpellCheck={false} controlStyle={{borderWidth:0,boxShadow:'none',backgroundColor:'transparent'}}/><IconButton label={showSecret?tr("隐藏 Cookie"):tr("显示 Cookie")} aria-pressed={showSecret} type="button" variant="ghost" icon={<Icon icon={showSecret?EyeSlashIcon:EyeIcon} size="sm"/>} onClick={()=>setShowSecret(!showSecret)}/></InputGroup></>}
   <Text type="supporting" color="secondary">{kind==='x11'?tr("本机需先启动 X 服务器；Cookie 为 32 位十六进制，留空须符合其访问控制配置。本机 Cookie 不会直接发给远端。"):kind==='vnc'?tr("认证由服务器请求，密码仅用于本次连接；离开此画面会释放 VNC 通道，不自动重连。"):tr("会打开 Windows 远程桌面。密码及证书由该客户端确认；关闭本页或 SSH 后停止本机通道。")}</Text>
   <Stack direction="horizontal" justify="end"><Button type="submit" label={kind==='vnc'?tr("连接 VNC"):kind==='rdp'?tr("在 Windows 远程桌面中打开"):tr("启动 X11 程序…")} isDisabled={!connected||!gateway.available()||!host.trim()||!Number.isInteger(Number(port))||Number(port)<1||Number(port)>65535||(kind==='x11'&&(!command.trim()||!!cookie&&!/^[0-9a-fA-F]{32}$/.test(cookie)))}/></Stack>
  </form></Stack>}
  {kind==='vnc'&&phase!=='idle'&&<Stack padding={3} paddingBlockStart={0} direction="horizontal" align="center" gap={2} wrap="wrap" style={{flexShrink:0}}><Switch label={tr("只读查看")} value={readOnly} onChange={setReadOnly}/><Button label="Ctrl+Alt+Del" variant="ghost" isDisabled={phase!=='connected'||readOnly} onClick={()=>rfb.current?.sendCtrlAltDel()}/><Button label={fullscreen?tr("退出全屏"):tr("全屏")} variant="ghost" isDisabled={phase!=='connected'} onClick={()=>{setError('');void(document.fullscreenElement?document.exitFullscreen():root.current?.requestFullscreen())?.catch(()=>setError(tr("未能进入全屏，请在当前窗口中查看。")))}}/></Stack>}
  <Stack ref={canvas} aria-label={tr("VNC 远程画面")} hidden={kind!=='vnc'||phase==='idle'} style={{display:kind==='vnc'&&phase!=='idle'?'flex':'none',flex:1,minHeight:0,minWidth:0,overflow:'hidden'}}/>
  {kind!=='vnc'&&phase!=='idle'&&<Stack padding={4} gap={3}><Text>{kind==='rdp'?tr("Windows 远程桌面已单独打开；请在该窗口中继续操作。"):tr("远程图形程序通过本机 X 服务器显示。")}</Text><Text type="supporting" color="secondary">{kind==='x11'?command:host+':'+port} {tr(" · 停止此通道或关闭 SSH 会结束图形连接，不自动重新启动。")}</Text></Stack>}
  {active&&confirmX11&&<Dialog isOpen purpose="form" padding={4} width="calc(var(--spacing-10) * 13)" onOpenChange={open=>{if(!open){setConfirmX11(false);setCookie('')}}}><Stack gap={3}><DialogHeader title={tr("启动远程 X11 程序？")} hasDivider={false}/><Text style={{overflowWrap:'anywhere'}}>{command}</Text><Text type="supporting">{tr("将允许当前服务器上的图形程序访问本机 X 显示 ")}{host}:{port}{tr("。请仅对信任的服务器开启。")}</Text><Stack direction="horizontal" justify="end" gap={2}><Button label={tr("取消")} variant="secondary" onClick={()=>{setConfirmX11(false);setCookie('')}}/><Button label={tr("确认启动")} onClick={()=>void open(true)}/></Stack></Stack></Dialog>}
  {active&&auth&&<EditorDialog title={tr("VNC 认证")} action="edit" hint={tr("仅用于本次连接，不保存。")} cancelLabel={tr("取消连接")} onClose={disconnect} formProps={{onSubmit:e=>{e.preventDefault();rfb.current?.sendCredentials({username:authUser,password,target:authTarget});setPassword('');setAuthUser('');setAuthTarget('');setShowSecret(false);setAuth(null)}}} actions={<Button type="submit" label={tr("继续连接")}/>}>{auth.includes('username')&&<TextInput hasAutoFocus label={tr("VNC 用户名")} value={authUser} onChange={setAuthUser} autoComplete="off"/>}{auth.includes('password')&&<InputGroup label={tr("VNC 密码")} size="md"><TextInput hasAutoFocus={!auth.includes('username')} label={tr("VNC 认证内容")} type={showSecret?'text':'password'} value={password} onChange={setPassword} autoComplete="off" hasAutoCorrect={false} hasSpellCheck={false} controlStyle={{borderWidth:0,boxShadow:'none',backgroundColor:'transparent'}}/><IconButton label={showSecret?tr("隐藏密码"):tr("显示密码")} aria-pressed={showSecret} type="button" variant="ghost" icon={<Icon icon={showSecret?EyeSlashIcon:EyeIcon} size="sm"/>} onClick={()=>setShowSecret(!showSecret)}/></InputGroup>}{auth.includes('target')&&<TextInput label={tr("目标会话")} value={authTarget} onChange={setAuthTarget} autoComplete="off"/>}</EditorDialog>}
 </Stack>
}
