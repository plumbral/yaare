import { useLayoutEffect, useState } from 'react'
import { Button } from '@astryxdesign/core/Button'
import { DialogHeader } from '@astryxdesign/core/Dialog'
import { Link } from '@astryxdesign/core/Link'
import { Stack } from '@astryxdesign/core/Stack'
import { Heading, Text } from '@astryxdesign/core/Text'
import { desktopAvailable } from '../../shared/bridge/application'
import { ApplicationService, type ApplicationReleaseNote } from '../../shared/generated/mygo'
import { useLocale } from '../../shared/i18n/locale'
import { tr } from '../../shared/i18n/messages'
import { Dialog } from '../../shared/ui/AppDialog'

const bundledNote: ApplicationReleaseNote = { ...import.meta.env.YAARE_RELEASE_NOTE, title: '', publishedAt: '' }
const releasesPage = 'https://github.com/plumbral/yaare/releases'
const headerLayout = { padding: 0 } as const

function ReleaseBody({ body }: { body: string }) {
  return body.trim().split(/\r?\n\s*\r?\n/).map((block, index) => {
    const heading = /^#{1,6} (.+)(?:\r?\n|$)/.exec(block)
    const text = block.slice(heading?.[0].length ?? 0).replace(/^[-*] /gm, '• ')
    return <Stack key={index} gap={1}>
      {heading && <Text type="label" weight="semibold">{heading[1]}</Text>}
      {text && <Text type="supporting" style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{text}</Text>}
    </Stack>
  })
}

export default function ReleaseNotesDialog({ isOpen, onClose, onAfterClose }: { isOpen: boolean; onClose: () => void; onAfterClose: () => void }) {
  const language = useLocale()
  const [notes, setNotes] = useState<ApplicationReleaseNote[]>([])
  const fetchHistory = import.meta.env.MODE !== 'browser' && desktopAvailable()
  const [loading, setLoading] = useState(fetchHistory)

  useLayoutEffect(() => {
    if (!isOpen || !fetchHistory) return
    let active = true
    setLoading(true)
    void ApplicationService.getReleaseNotes()
      .then(result => { if (active) setNotes(result ?? []) })
      .catch(() => {})
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [isOpen, fetchHistory])

  const releases = notes.length > 0 ? notes : [bundledNote]
  const singleRelease = releases.length === 1 ? releases[0] : undefined
  return <Dialog isOpen={isOpen} onAfterClose={onAfterClose} purpose="info" padding={4} width="min(calc(var(--spacing-10) * 16), 92vw)" maxHeight="85dvh" onOpenChange={open => { if (!open) onClose() }}>
    <Stack gap={4} style={{ minHeight: 0 }}>
      <DialogHeader {...headerLayout} title={<>
        {tr('更新日志')}
        {singleRelease && <>{' '}<Text as="span" type="supporting" color="secondary"
          style={{ marginInlineStart: 'var(--spacing-2)', overflowWrap: 'anywhere' }}>{singleRelease.tag}</Text></>}
      </>}/>
      <Stack gap={4} role="region" aria-label={tr('更新日志')} tabIndex={0} aria-busy={loading}
        style={{ overflowY: 'auto', overscrollBehavior: 'contain', maxHeight: '56dvh', scrollbarGutter: 'stable' }}>
        {releases.map(note => {
          const title = note.title || note.tag
          const showTitle = !singleRelease || title !== note.tag
          return <Stack as="article" key={note.tag} gap={2} paddingBlock={2} paddingInlineEnd={2}>
            {(showTitle || note.publishedAt) && <Stack direction="horizontal" gap={2} wrap="wrap"
              style={{ alignItems: 'baseline', overflowWrap: 'anywhere' }}>
              {showTitle && <Heading level={5} accessibilityLevel={3} weight="semibold">{title}</Heading>}
              {!singleRelease && title !== note.tag && <Text type="supporting" color="secondary">{note.tag}</Text>}
              {note.publishedAt && <Text type="supporting" color="secondary">{new Date(note.publishedAt).toLocaleDateString(language)}</Text>}
            </Stack>}
            <ReleaseBody body={note.body}/>
          </Stack>
        })}
      </Stack>
      <Stack direction="horizontal" gap={2} align="center" justify="between" wrap="wrap">
        <Link href={releasesPage} isExternalLink newTabLabel={tr('在浏览器中打开')} isStandalone>GitHub Releases</Link>
        {loading && <Text type="supporting" role="status" color="secondary">{tr('正在读取发布记录…')}</Text>}
        <Button label={tr('关闭')} size="md" variant="secondary" onClick={onClose}/>
      </Stack>
    </Stack>
  </Dialog>
}
