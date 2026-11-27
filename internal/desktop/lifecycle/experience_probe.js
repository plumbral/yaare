(() => {
  const media = matchMedia('(prefers-reduced-motion: reduce)');
  const start = performance.now(), boot = crypto.randomUUID(), started = new Date().toISOString();
  const events = [], geometry = [], postGeometry = [], paints = [], eventTiming = [], animation = [], candidates = [], violations = [];
  let until = start + 1000, frames = 0, badGeometry = 0, keyCount = 0, last = 0;
  let resizeCount = 0, lastMotion = media.matches, confirmedGeometry = 0, postFrames = 0, postPending = false;
  const reorders = [], seenReorders = new WeakSet();
  let dragFrames = 0, markerFrames = 0, listScrollMin = Infinity, listScrollMax = 0;
  const push = (list, item, max = 512) => { list.push(item); if (list.length > max) list.shift(); };
  const number = n => Math.round(n * 100) / 100;
  const box = e => { const r = e.getBoundingClientRect(); return {x:number(r.x), y:number(r.y), width:number(r.width), height:number(r.height)}; };
  const visible = e => { const r=e.getBoundingClientRect(); return r.width > 0 && r.height > 0; };
  const panels = () => ['server-panel','tools-panel','host-groups-panel','command-groups-panel'].map(id => {
    const e = document.getElementById(id); if (!e) return null;
    const s = getComputedStyle(e);
    return {id, ...box(e), display:s.display, visibility:s.visibility, inert:e.inert,
      transition:s.transitionProperty, duration:s.transitionDuration, animationDuration:s.animationDuration,
      animations:e.getAnimations().filter(a=>a.playState==='running').length};
  }).filter(Boolean);
  const ancestry = id => {
    const path=[];let e=typeof id==='string'?document.getElementById(id):id;
    while(e){const s=getComputedStyle(e);path.push({tag:e.tagName,id:e.id,role:e.getAttribute('role'),inert:e.inert,hidden:e.hidden,ariaHidden:e.getAttribute('aria-hidden'),display:s.display,visibility:s.visibility});e=e.parentElement;}
    return path;
  };
  const sample = (diagnostics = false) => {
    const root = document.querySelector('#terminal-workspace [role="tabpanel"]');
    const bounds = root?.getBoundingClientRect();
    const nodes = [...document.querySelectorAll('[aria-label^="终端窗格 "]')].filter(visible);
    const rects = nodes.map(e=>e.getBoundingClientRect());
    const outside = !!bounds && rects.some(r=>r.left<bounds.left-1||r.top<bounds.top-1||r.right>bounds.right+1||r.bottom>bounds.bottom+1);
    let overlaps = 0;
    for(let i=0;i<rects.length;i++)for(let j=i+1;j<rects.length;j++){
      if(Math.min(rects[i].right,rects[j].right)-Math.max(rects[i].left,rects[j].left)>1 && Math.min(rects[i].bottom,rects[j].bottom)-Math.max(rects[i].top,rects[j].top)>1)overlaps++;
    }
    return {at:number(performance.now()-start), width:innerWidth, height:innerHeight, dpr:devicePixelRatio,
      reduced:media.matches, focused:document.hasFocus(), visibility:document.visibilityState,
      root:root?box(root):null, documentWidth:document.documentElement.scrollWidth, clientWidth:document.documentElement.clientWidth,
      visiblePanes:rects.length, outside, overlaps, overflow:document.documentElement.scrollWidth>innerWidth,
      panes:nodes.map(box), panels:panels(),
      controls:diagnostics?['split-menu','sftp-panel-toggle','server-panel-toggle'].map(id=>{const e=document.getElementById(id);return e?{id,...box(e),ancestors:ancestry(id)}:null;}).filter(Boolean):undefined,
      terminalAncestors:diagnostics?ancestry('terminal-workspace'):undefined,
      toolAncestry:diagnostics?['panel-commands'].map(selector=>{const e=selector.startsWith('[')?document.querySelector(selector):document.getElementById(selector);return e?{kind:selector,box:box(e),ancestors:ancestry(e)}:null}).filter(Boolean):undefined,
      dialogs:[...document.querySelectorAll('dialog[open]')].map(e=>({ ...box(e),opacity:getComputedStyle(e).opacity }))};
  };
  const activity = kind => { until = performance.now()+1000; push(events,{at:number(performance.now()-start),kind,reduced:media.matches}); };
  media.addEventListener('change',()=>{lastMotion=media.matches;activity('motion-preference');});
  addEventListener('resize',()=>{resizeCount++;activity('resize');});
  document.addEventListener('pointerdown',()=>activity('pointer'),true);
  document.addEventListener('keydown',e=>{
    if(!e.isTrusted)return;
    keyCount++;activity('keyboard');
    const at=performance.now(),focused=document.hasFocus();
    requestAnimationFrame(()=>requestAnimationFrame(()=>push(paints,{ms:number(performance.now()-at),focused,visible:document.visibilityState==='visible'})));
  },true);
  for(const type of ['transitionrun','transitionend','transitioncancel','animationstart','animationend']){
    document.addEventListener(type,e=>{
      const t=e.target;
      const kind=t instanceof Element&&t.closest('dialog')?'dialog':t instanceof Element&&t.closest('#host-groups-panel')?'groups':'other';
      push(animation,{at:number(performance.now()-start),event:type,kind,reduced:media.matches});activity('animation');
    },true);
  }
  if(PerformanceObserver.supportedEntryTypes.includes('event')){
    new PerformanceObserver(list=>{for(const e of list.getEntries())push(eventTiming,{duration:e.duration,processing:number(e.processingEnd-e.processingStart),interaction:!!e.interactionId});}).observe({type:'event',durationThreshold:16});
  }
  const frame = now => {
    if(document.visibilityState==='visible'&&now<until){
      for(const row of document.querySelectorAll('[data-reorder-animating]'))for(const a of row.getAnimations()){
        if(seenReorders.has(a))continue;seenReorders.add(a);
        const timing=a.effect?.getComputedTiming();
        push(reorders,{at:number(now-start),duration:timing?.duration,progress:timing?.progress,reduced:media.matches},128);
      }
      if(document.querySelector('[data-reorder-dragging="dragging"]'))dragFrames++;
      const marker=document.querySelector('[data-host-order-marker]');if(marker&&visible(marker))markerFrames++;
      const list=document.querySelector('#panel-hosts [data-windowed-list]');if(list){listScrollMin=Math.min(listScrollMin,list.scrollTop);listScrollMax=Math.max(listScrollMax,list.scrollTop);}
      const s=sample();frames++;
      if(s.outside||s.overlaps||s.overflow){
        badGeometry++;push(candidates,s,64);
      }
      if(!postPending){postPending=true;setTimeout(()=>{
        postPending=false;const next={...sample(),phase:'after-render-task'};postFrames++;
        push(postGeometry,next,128);
        if(next.outside||next.overlaps||next.overflow){confirmedGeometry++;push(violations,next,64);}
      },0);}
      push(geometry,{...s,frameMs:last?number(now-last):0},400);
    }
    last=now;requestAnimationFrame(frame);
  };
  requestAnimationFrame(frame);
  Object.defineProperty(globalThis,'__yaareExperienceSnapshot',{value:()=>({format:2,boot,started,
    note:'Read-only native probe; no terminal text, input values, key identities, URLs or credentials recorded. Instrumented timing is separate from uninstrumented resource baselines.',
    elapsed:number(performance.now()-start),reducedMotion:lastMotion,frames,postFrames,badGeometry,confirmedGeometry,resizeCount,keyCount,
    reorders:[...reorders],dragFrames,markerFrames,listScrollMin:Number.isFinite(listScrollMin)?number(listScrollMin):0,listScrollMax:number(listScrollMax),current:sample(true),events:[...events],geometry:[...geometry],postGeometry:[...postGeometry],candidates:[...candidates],violations:[...violations],keyToRenderingOpportunity:[...paints],eventTiming:[...eventTiming],animation:[...animation]})});
})();
