export interface DirectoryHistory {paths:string[];index:number;lastFollow:number}
export const emptyHistory=():DirectoryHistory=>({paths:[],index:-1,lastFollow:0})
export function visitDirectory(previous:DirectoryHistory,path:string,kind:'navigate'|'follow'|'history'='navigate',index?:number,now=Date.now()):DirectoryHistory{
 if(kind==='history'&&index!==undefined&&index>=0&&index<previous.paths.length){const paths=[...previous.paths];paths[index]=path;return {paths,index,lastFollow:0}}
 if(previous.paths[previous.index]===path)return previous
 const paths=previous.paths.slice(0,previous.index+1)
 if(kind==='follow'&&previous.lastFollow>0&&now-previous.lastFollow<=1000&&paths.length>1)paths[paths.length-1]=path
 else paths.push(path)
 if(paths.length>64)paths.splice(0,paths.length-64)
 return {paths,index:paths.length-1,lastFollow:kind==='follow'?now:0}
}
export function directoryStep(history:DirectoryHistory,step:number){const index=history.index+step;return index>=0&&index<history.paths.length?{index,path:history.paths[index]!}:null}
export function directoryCrumbs(path:string,rootLabel='/'){
 const absolute=path.startsWith('/'),segments=path.split('/').filter(Boolean)
 if(!absolute)return [{label:path||'.',path:path||'.'}]
 return [{label:rootLabel,path:'/'},...segments.map((label,index)=>({label,path:'/'+segments.slice(0,index+1).join('/')}))]
}
