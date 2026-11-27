import {Stack} from '@astryxdesign/core/Stack'
import {Text} from '@astryxdesign/core/Text'
import {Icon} from '@astryxdesign/core/Icon'
import {FolderIcon} from '@heroicons/react/24/outline'
import {tr} from '../i18n/messages'
import {useLocale} from '../i18n/locale'

export default function GroupPath({name}:{name:string}){
 useLocale()
 const parts=name.split('/')
 return <Stack as="ol" aria-label={tr('分组层级')} gap={2} style={{listStyle:'none',margin:0,padding:0,minWidth:0}}>
  {parts.map((part,index)=><Stack as="li" key={index} direction="horizontal" gap={2} align="start" style={{paddingInlineStart:`calc(var(--spacing-4) * ${index})`,minWidth:0}}>
   <Icon icon={FolderIcon} size="sm" style={{flexShrink:0,marginBlockStart:'var(--spacing-1)'}}/>
   <Text type="body" weight={index===parts.length-1?'semibold':undefined} color={index===parts.length-1?'primary':'secondary'} style={{minWidth:0,overflowWrap:'anywhere'}}>{part}</Text>
  </Stack>)}
 </Stack>
}
