import {tr} from '../../shared/i18n/messages'
export const unassignedGroup='未分组'
export const groupLabel=(name:string)=>name.split('/').join(' › ')
export const groupLeaf=(name:string)=>name.split('/').at(-1)??name
export const groupParent=(name:string)=>name.includes('/')?name.slice(0,name.lastIndexOf('/')):''
export function validGroupLeaf(name:string){return !!name&&name.trim()===name&&[...name].length<=32&&!/[\p{Cc}\u2028\u2029/\\]/u.test(name)&&name!=='.'&&name!=='..'&&name!==unassignedGroup}
export function childGroupName(parent:string,leaf:string,groups:readonly string[]){
 if(!validGroupLeaf(leaf))throw Error(tr('请输入 1–32 个字符的分组名称，不含斜杠。'))
 if(parent&&(!groups.includes(parent)||parent===unassignedGroup))throw Error(tr('父分组已不存在，请重新选择。'))
 if(parent.split('/').filter(Boolean).length>=8)throw Error(tr('分组最多支持 8 层。'))
 const full=parent?parent+'/'+leaf:leaf
 if(groups.includes(full))throw Error(tr('此分组已存在，请换一个名称。'))
 return full
}
export interface GroupChoice {value:string;name:string;label:string;create?:boolean}
export function groupChoices(groups:readonly string[],query:string):GroupChoice[]{
 const text=query.trim(),names=[...new Set(groups)].filter(g=>g&&g!==unassignedGroup)
 const matched=names.filter(g=>(g+' '+groupLabel(g)).toLocaleLowerCase().includes(text.toLocaleLowerCase())).sort((a,b)=>a.localeCompare(b))
 const exact=names.find(g=>g===text||groupLabel(g)===text)
 const options:GroupChoice[]=matched.map(name=>({value:'group:'+name,name,label:groupLabel(name)}))
 if(exact){const at=options.findIndex(o=>o.name===exact);options.unshift(...options.splice(at,1))}
 else if(validGroupLeaf(text))options.unshift({value:'create:'+text,name:text,label:tr('创建「{p0}」',text),create:true})
 if(!text)options.unshift({value:'group:'+unassignedGroup,name:unassignedGroup,label:tr('不指定分组')})
 return options
}
export function isComposingEnter(event:{isComposing?:boolean;keyCode?:number}){return !!event.isComposing||event.keyCode===229}
