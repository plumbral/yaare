import {useEffect,useRef,useState,type PointerEvent,type RefObject} from 'react'
import type {HostProfile} from '../workspace/mockGateway'
import {rowMotion} from '../../shared/ui/listMotion'

type Gesture={id:number;host:HostProfile;button:HTMLButtonElement;row:HTMLElement;scroll:HTMLElement;items:readonly HostProfile[];peers:string[];ranks:Map<string,number>;from:number;to:number;y:number;startY:number;startScroll:number;offset:number;dragged:boolean;frame:number;time:number}
export default function useHostReorderDrag({list,items,allowed,onDrop,onStart}:{list:RefObject<HTMLElement|null>;items:readonly HostProfile[];allowed:boolean;onDrop:(host:HostProfile,to:number)=>Promise<boolean>;onStart:()=>void}){
 const marker=useRef<HTMLElement|null>(null),gesture=useRef<Gesture|null>(null),suppressed=useRef(false),[key,setKey]=useState<string>()
 const latest=useRef({items,allowed,onDrop,onStart});latest.current={items,allowed,onDrop,onStart}
 function clean(g:Gesture){
  cancelAnimationFrame(g.frame);gesture.current=null;setKey(undefined)
  if(marker.current)marker.current.style.display='none'
  if(g.button.hasPointerCapture(g.id))g.button.releasePointerCapture(g.id)
 }
 function cancel(){const g=gesture.current;if(!g)return;clean(g)
  if(g.dragged){const offset=Number.parseFloat(g.row.style.translate.split(' ')[1]??'0')||0;g.row.style.translate='';g.row.style.zIndex='';g.row.style.boxShadow='';g.row.style.backgroundColor='';delete g.row.dataset.reorderDragging;rowMotion(g.row,offset)}
 }
 useEffect(()=>()=>cancel(),[])
 useEffect(()=>{const g=gesture.current;if(g&&(!allowed||items!==g.items)){cancel()}},[allowed,items])
 function place(g:Gesture){
  const area=g.scroll.getBoundingClientRect(),container=g.scroll.querySelector<HTMLElement>('[data-windowed-items]');if(!container)return
  const origin=container.getBoundingClientRect().top
  const nodes=[...container.querySelectorAll<HTMLElement>('[data-row-key]')].map(row=>({row,index:g.ranks.get(row.dataset.rowKey??'')??-1,top:origin+Number(row.dataset.rowStart),height:row.offsetHeight})).filter(row=>row.index>=0).sort((a,b)=>a.index-b.index)
  if(!nodes.length)return
  const y=Math.max(area.top,Math.min(area.bottom,g.y)),after=nodes.find(row=>y<row.top+row.height/2)
  const insert=after?after.index:nodes[nodes.length-1]!.index+1
  g.to=Math.max(0,Math.min(g.peers.length-1,insert-(insert>g.from?1:0)))
  if(marker.current){const at=after?after.top:nodes[nodes.length-1]!.top+nodes[nodes.length-1]!.height
   Object.assign(marker.current.style,{display:g.to===g.from?'none':'flex',top:`${Math.max(area.top,Math.min(area.bottom,at))}px`,left:`${area.left}px`,width:`${area.width}px`})}
  g.row.style.translate=`0 ${g.offset+g.y-g.startY+g.scroll.scrollTop-g.startScroll}px`
 }
 function tick(time:number){const g=gesture.current;if(!g||!g.dragged)return
  const bounds=g.scroll.getBoundingClientRect(),edge=Math.min(g.row.offsetHeight,bounds.height/3),dt=Math.min(32,time-g.time||16);g.time=time
  let speed=g.y<bounds.top+edge?-Math.min(1,(bounds.top+edge-g.y)/edge):g.y>bounds.bottom-edge?Math.min(1,(g.y-bounds.bottom+edge)/edge):0
  const boundary=g.scroll.querySelector<HTMLElement>(`[data-row-key="${speed<0?g.peers[0]:g.peers[g.peers.length-1]}"]`)
  if(boundary){const box=boundary.getBoundingClientRect();if(speed<0&&box.top>=bounds.top||speed>0&&box.bottom<=bounds.bottom)speed=0}
  if(speed)g.scroll.scrollTop+=speed*dt*.65
  place(g);g.frame=requestAnimationFrame(tick)
 }
 function grab(event:PointerEvent<HTMLButtonElement>,host:HostProfile){
  if(event.button!==0||!latest.current.allowed||gesture.current)return
  const row=event.currentTarget.closest<HTMLElement>('[data-row-key]'),scroll=list.current?.querySelector<HTMLElement>('[data-windowed-list]');if(!row||!scroll)return
  suppressed.current=false
  const peers=latest.current.items.filter(h=>!!h.pinned===!!host.pinned).map(h=>h.id)
  const g:Gesture={id:event.pointerId,host,button:event.currentTarget,row,scroll,items:latest.current.items,peers,ranks:new Map(peers.map((id,index)=>[id,index])),from:peers.indexOf(host.id),to:peers.indexOf(host.id),y:event.clientY,startY:event.clientY,startScroll:scroll.scrollTop,offset:0,dragged:false,frame:0,time:0}
  gesture.current=g;setKey(host.id);event.currentTarget.setPointerCapture(event.pointerId)
 }
 useEffect(()=>{
  const track=(g:Gesture,event:globalThis.PointerEvent)=>{g.y=event.clientY
   if(!g.dragged&&Math.abs(g.y-g.startY)>5){g.dragged=true;suppressed.current=true;latest.current.onStart();g.offset=Number.parseFloat(getComputedStyle(g.row).translate.split(' ')[1]??'0')||0;g.row.getAnimations().forEach(a=>a.cancel());g.row.dataset.reorderDragging='dragging';g.row.style.zIndex='3';g.row.style.backgroundColor='var(--color-background-body)';g.row.style.boxShadow='var(--shadow-low)';g.frame=requestAnimationFrame(tick)}
   if(g.dragged){event.preventDefault();place(g)}
  }
  const move=(event:globalThis.PointerEvent)=>{const g=gesture.current;if(g&&event.pointerId===g.id)track(g,event)}
  const up=(event:globalThis.PointerEvent)=>{const g=gesture.current;if(!g||event.pointerId!==g.id)return
   track(g,event)
   if(!g.dragged){clean(g);return}
   g.row.dataset.reorderDragging='settling';cancelAnimationFrame(g.frame);gesture.current=null
   if(marker.current)marker.current.style.display='none'
   if(g.button.hasPointerCapture(g.id))g.button.releasePointerCapture(g.id)
   void (g.to===g.from?Promise.resolve(false):latest.current.onDrop(g.host,g.to)).finally(()=>{const offset=Number.parseFloat(g.row.style.translate.split(' ')[1]??'0')||0
    if(g.row.dataset.reorderDragging==='settling'){g.row.style.translate='';delete g.row.dataset.reorderDragging;rowMotion(g.row,offset)}
    g.row.style.zIndex='';g.row.style.boxShadow='';g.row.style.backgroundColor='';setKey(undefined)
   })
  }
  const keydown=(event:KeyboardEvent)=>{if(event.key==='Escape'&&gesture.current){event.preventDefault();event.stopPropagation();cancel()}}
  const lost=(event:globalThis.PointerEvent)=>{if(gesture.current?.id===event.pointerId)cancel()}
  document.addEventListener('pointermove',move,{passive:false});document.addEventListener('pointerup',up);document.addEventListener('pointercancel',lost);document.addEventListener('lostpointercapture',lost);document.addEventListener('keydown',keydown,true);window.addEventListener('blur',cancel)
  return()=>{document.removeEventListener('pointermove',move);document.removeEventListener('pointerup',up);document.removeEventListener('pointercancel',lost);document.removeEventListener('lostpointercapture',lost);document.removeEventListener('keydown',keydown,true);window.removeEventListener('blur',cancel)}
 },[])
 return{grab,key,marker,suppressClick:()=>{const value=suppressed.current;suppressed.current=false;return value}}
}
