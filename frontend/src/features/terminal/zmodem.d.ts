declare module 'zmodem.js'{
 export interface Details{name:string;size:number;mtime?:Date;files_remaining?:number;bytes_remaining?:number}
 export interface Offer{get_details():Details;accept(options:{on_input:(bytes:number[])=>void}):Promise<unknown>;skip():Promise<unknown>|void}
 export interface Outgoing{send(bytes:number[]):void;end(bytes?:number[]):Promise<unknown>}
 export interface Session{get_pending_input():number[];aborted():boolean;type:'send'|'receive';on(event:'offer',callback:(offer:Offer)=>void):void;on(event:'session_end',callback:()=>void):void;start():void;send_offer(details:Details):Promise<Outgoing|undefined>;close():Promise<unknown>}
 export interface Detection{confirm():Session;deny():void;is_valid():boolean;get_session_role():'send'|'receive'}
 export class Sentry{constructor(options:{to_terminal:(bytes:number[])=>void;sender:(bytes:number[])=>void;on_detect:(detection:Detection)=>void;on_retract:()=>void});consume(bytes:Uint8Array|number[]):void}
 const zmodem:{Sentry:typeof Sentry};export default zmodem
}
