import {useEffect,useId,useMemo,useRef,useState,useCallback} from 'react'
import {Stack} from '@astryxdesign/core/Stack'
import {TextInput} from '@astryxdesign/core/TextInput'
import {Button} from '@astryxdesign/core/Button'
import {IconButton} from '@astryxdesign/core/IconButton'
import {Icon} from '@astryxdesign/core/Icon'
import {InputGroup} from '@astryxdesign/core/InputGroup'
import {Text} from '@astryxdesign/core/Text'
import {VisuallyHidden} from '@astryxdesign/core/VisuallyHidden'
import {useCombobox} from '@astryxdesign/core/Selector'
import {usePopover} from '@astryxdesign/core/Popover'
import {ChevronDownIcon,XMarkIcon} from '@heroicons/react/24/outline'
import {tr} from '../../shared/i18n/messages'
import {useLocale} from '../../shared/i18n/locale'
import {groupChoices,groupLabel,isComposingEnter,unassignedGroup} from './groupCreation'

export default function GroupCombobox({value,groups,onChange,disabled,active,error}:{value:string;groups:readonly string[];onChange:(name:string)=>void;disabled:boolean;active:boolean;error?:string}){
 useLocale()
 const input=useRef<HTMLInputElement>(null),id=useId(),composing=useRef(false)
 const display=value===unassignedGroup?'':groupLabel(value)
 const [query,setQuery]=useState(display),[editing,setEditing]=useState(false)
 const committed=useRef(display);committed.current=display
 const restore=useCallback(()=>{setQuery(committed.current);setEditing(false)},[])
 const popup=usePopover({role:'none',isModal:false,hasAutoFocus:false,hasCloseButton:false,onHide:restore,padding:1})
 const options=useMemo(()=>groupChoices([...groups,value],editing?query:''),[groups,value,editing,query])
 const choose=(choice:string)=>{
  const item=options.find(o=>o.value===choice);if(!item||disabled)return
  const name=item.name
  committed.current=name===unassignedGroup?'':groupLabel(name);setQuery(committed.current);setEditing(false);onChange(name)
 }
 const combo=useCombobox({selectableItems:options,isOpen:popup.isOpen,onOpen:()=>{if(disabled)return false;popup.show({skipAutoFocus:true})},onClose:popup.hide,listboxId:id,value:'group:'+value,onSelect:choose,hasSearch:true,isDisabled:disabled})
 useEffect(()=>{if(!popup.isOpen){setQuery(display);setEditing(false)}},[display,popup.isOpen])
 useEffect(()=>{if(!active||disabled)popup.hide()},[active,disabled])
 useEffect(()=>{if(popup.isOpen&&combo.highlightedIndex>=0)document.getElementById(combo.getItemId(combo.highlightedIndex))?.scrollIntoView({block:'nearest'})},[combo.highlightedIndex,popup.isOpen,query])
 return <Stack ref={popup.triggerRef} width="100%" gap={1}>
  <InputGroup label={tr('分组')} isOptional size="md" isDisabled={disabled}>
   <TextInput ref={input} id="connection-group" label={tr('分组')} isLabelHidden size="md" value={query} placeholder={tr('搜索或新建分组…')} autoComplete="off" hasSpellCheck={false} hasAutoCorrect={false} autoCapitalize="none"
    role="combobox" aria-autocomplete="list" aria-expanded={popup.isOpen} aria-controls={popup.isOpen?id:undefined} aria-activedescendant={popup.isOpen&&combo.highlightedIndex>=0&&combo.highlightedIndex<options.length?combo.getItemId(combo.highlightedIndex):undefined}
    aria-invalid={!!error||undefined} aria-describedby={error?id+'-error':undefined} isDisabled={disabled}
    controlStyle={{borderWidth:0,boxShadow:'none',backgroundColor:'transparent',marginInlineStart:0}}
    onCompositionStart={()=>{composing.current=true}} onCompositionEnd={()=>{composing.current=false}}
    onClick={()=>{
     if(disabled||popup.isOpen)return
     popup.show({skipAutoFocus:true})
     combo.setHighlightedIndex(Math.max(0,options.findIndex(option=>option.name===value)))
    }}
    onChange={text=>{setQuery(text);setEditing(true);if(!disabled)popup.show({skipAutoFocus:true});combo.setHighlightedIndex(0)}}
    onKeyDown={event=>{
     if(composing.current||isComposingEnter(event.nativeEvent)){event.stopPropagation();return}
     if(['Home','End','ArrowLeft','ArrowRight'].includes(event.key))return
     if(event.key==='Enter'){
      event.preventDefault();event.stopPropagation()
      if(popup.isOpen&&combo.highlightedIndex>=0&&options[combo.highlightedIndex])combo.onItemSelect(options[combo.highlightedIndex])
      else if(editing&&options[0])combo.onItemSelect(options[0])
      else{popup.show({skipAutoFocus:true});combo.setHighlightedIndex(0)}
      return
     }
     combo.onKeyDown(event)
    }}/>
   {!!query&&<IconButton label={tr('清除分组')} tooltip={tr('清除分组')} type="button" size="md" variant="ghost" icon={<Icon icon={XMarkIcon} size="sm"/>} isDisabled={disabled} onClick={()=>{committed.current='';onChange(unassignedGroup);setQuery('');setEditing(false);popup.hide();input.current?.focus()}}/>}
   <IconButton label={tr('选择分组')} tooltip={tr('选择分组')} type="button" size="md" variant="ghost" icon={<Icon icon={ChevronDownIcon} size="sm"/>} isDisabled={disabled} onClick={()=>{input.current?.focus();if(popup.isOpen)popup.hide();else popup.show({skipAutoFocus:true});combo.setHighlightedIndex(Math.max(0,options.findIndex(option=>option.name===value)))}}/>
  </InputGroup>
  {error&&<Text id={id+'-error'} type="supporting" color="secondary" role="alert">{error}</Text>}
  {popup.render(<Stack {...combo.getListboxProps()} aria-label={tr('分组')} gap={1} style={{maxHeight:'min(calc(var(--spacing-10) * 8), 45dvh)',overflowY:'auto'}}>
   {options.map((option,index)=><Button key={option.value} {...combo.getOptionProps(index)} label={option.label} type="button" tabIndex={-1} size="md" width="100%" variant={combo.highlightedIndex===index?'secondary':'ghost'} style={{justifyContent:'flex-start',textAlign:'start'}}/>)}
   {!options.length&&<Text type="supporting" color="secondary">{tr('请输入有效名称以创建分组，或选择已有分组。')}</Text>}
  </Stack>,{placement:'below',alignment:'start',style:{width:'calc(var(--spacing-10) * 8)',maxWidth:'calc(100dvw - var(--spacing-8))'}})}
  <VisuallyHidden role="status">{popup.isOpen?tr('{p0} 个分组可选',options.length):''}</VisuallyHidden>
 </Stack>
}
