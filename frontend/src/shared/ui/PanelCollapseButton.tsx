import { tr } from '../i18n/messages'
import { useLocale, getLanguage } from '../i18n/locale'
import {IconButton} from '@astryxdesign/core/IconButton'
import {Icon} from '@astryxdesign/core/Icon'
import {ChevronDoubleLeftIcon,ChevronDoubleRightIcon} from '@heroicons/react/24/outline'

export default function PanelCollapseButton({side,onCollapse}:{side:'start'|'end';onCollapse:()=>void}){
  useLocale()

  const label=side==='start'?tr("收起服务器列表"):tr("收起文件管理")
  return <IconButton label={label} tooltip={label} size="md" variant="ghost" aria-controls={side==='start'?'server-panel':'tools-panel'}
    icon={<Icon icon={side==='start'?ChevronDoubleLeftIcon:ChevronDoubleRightIcon} size="sm"/>} onClick={onCollapse}/>
}
