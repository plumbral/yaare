import { tr } from '../../shared/i18n/messages'
import { useLocale, getLanguage } from '../../shared/i18n/locale'
import {useEffect,useRef,useState} from 'react'
import {Stack,StackItem} from '@astryxdesign/core/Stack'
import {Text} from '@astryxdesign/core/Text'
import {EditorState} from '@codemirror/state'
import {EditorView,lineNumbers} from '@codemirror/view'
import {syntaxHighlighting} from '@codemirror/language'
import {MergeView,unifiedMergeView} from '@codemirror/merge'
import {editorTheme,colors} from '../../shared/ui/codeEditorTheme'
import {registerCodeEditorContextActions} from '../../shared/ui/codeEditorContextActions'
import {remoteSyntax} from './RemoteCodeEditor'
const diffTheme=EditorView.theme({'&.cm-merge-a .cm-changedLine,.cm-deletedChunk':{backgroundColor:'var(--color-error-muted)'},'&.cm-merge-b .cm-changedLine,.cm-inlineChangedLine':{backgroundColor:'var(--color-success-muted)'},'&.cm-merge-a .cm-changedText,.cm-deletedChunk .cm-deletedText':{textDecoration:'line-through',background:'none'},'&.cm-merge-b .cm-changedText':{textDecoration:'underline',background:'none'},'.cm-changeGutter':{color:'var(--color-text-secondary)'},'.cm-collapseWidget':{backgroundColor:'var(--color-background-secondary)',color:'var(--color-text-secondary)'}})
export default function FileDiffEditor({left,right,path}:{left:string;right:string;path:string}){
  useLocale()

 const mount=useRef<HTMLElement>(null),[narrow,setNarrow]=useState(false),scroll=useRef(0)
 useEffect(()=>{const el=mount.current;if(!el)return;const observer=new ResizeObserver(entries=>{const width=entries[0]?.contentRect.width??0;if(width)setNarrow(width<640)});observer.observe(el);return()=>observer.disconnect()},[])
 useEffect(()=>{const el=mount.current;if(!el)return;const extensions=(label:string)=>[EditorState.readOnly.of(true),EditorView.editable.of(false),EditorView.contentAttributes.of({'aria-label':label,tabindex:'0'}),lineNumbers(),syntaxHighlighting(colors),...remoteSyntax(path),EditorView.lineWrapping,editorTheme,diffTheme]
  if(narrow){const view=new EditorView({parent:el,state:EditorState.create({doc:right,extensions:[...extensions(tr("统一文本差异：本地删除与远程新增")),unifiedMergeView({original:left,mergeControls:false,syntaxHighlightDeletions:true,diffConfig:{scanLimit:1000,timeout:200}})]})});view.scrollDOM.scrollTop=scroll.current;const off=registerCodeEditorContextActions(view);return()=>{scroll.current=view.scrollDOM.scrollTop;off();view.destroy()}}
  const view=new MergeView({parent:el,a:{doc:left,extensions:extensions(tr("本地文件快照，只读"))},b:{doc:right,extensions:extensions(tr("远程文件快照，只读"))},diffConfig:{scanLimit:1000,timeout:200}});Object.assign(view.dom.style,{height:'100%',overflow:'auto'});view.dom.scrollTop=scroll.current;const offs=[registerCodeEditorContextActions(view.a),registerCodeEditorContextActions(view.b)];return()=>{scroll.current=view.dom.scrollTop;offs.forEach(off=>off());view.destroy()}
 },[left,right,path,narrow])
 return <Stack gap={2} style={{minHeight:0,minWidth:0,height:'100%'}}>{narrow?<Text type="supporting" color="secondary">{tr("逐段差异：本地（删除） → 远程（新增）")}</Text>:<Stack direction="horizontal" gap={2}><StackItem size="fill"><Text type="supporting" color="secondary">{tr("本地（只读）")}</Text></StackItem><StackItem size="fill"><Text type="supporting" color="secondary">{tr("远程（只读）")}</Text></StackItem></Stack>}<StackItem ref={mount} size="fill" aria-label={narrow?tr("统一差异：删除为本地、新增为远程"):tr("左右差异：本地与远程")} style={{minHeight:0,minWidth:0,overflow:'hidden',border:'var(--border-width) solid var(--color-border-emphasized)',borderRadius:'var(--radius-element)'}}/></Stack>
}
