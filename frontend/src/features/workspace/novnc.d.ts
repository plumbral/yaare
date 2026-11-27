declare module '@novnc/novnc' {
 export default class RFB extends EventTarget {
  constructor(target:HTMLElement,channel:unknown,options?:{shared?:boolean;credentials?:{username?:string;password?:string;target?:string}})
  viewOnly:boolean;scaleViewport:boolean;resizeSession:boolean;qualityLevel:number;compressionLevel:number;focusOnClick:boolean
  disconnect():void;focus():void;sendCredentials(credentials:{username?:string;password?:string;target?:string}):void;sendCtrlAltDel():void;clipboardPasteFrom(text:string):void
 }
}
declare module '@yaare/novnc-logging' {export function initLogging(level:'none'):void}
