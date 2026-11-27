;(function () {
 var saved=window.__yaareStartupAppearance;
 if(!saved && document.currentScript?.dataset.mode !== 'browser'){
  try {
   saved=JSON.parse(localStorage.getItem('yaare.preview.preferences.v1')||'null');
  }catch(_){}
 }
 var mode=saved&&saved.mode;
 if(mode!=='light'&&mode!=='dark')mode='system';
 var scheme=mode==='system'?'light dark':mode;
 document.querySelector('meta[name="color-scheme"]').content=scheme;
 document.documentElement.style.colorScheme=scheme;
 document.documentElement.dataset.theme=mode;
 var palettes={paper:'yaare-paper',purple:'yaare-purple',blue:'yaare-blue',green:'yaare-green'};
 var palette=saved&&saved.interfacePalette;
 document.documentElement.dataset.astryxTheme=Object.hasOwn(palettes,palette)?palettes[palette]:'yaare';
})();
