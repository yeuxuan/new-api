/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import {
  Alert02Icon,
  ArrowUpRight01Icon,
  CheckmarkCircle02Icon,
  CodeIcon,
  Copy01Icon,
  RefreshIcon,
  Task01Icon,
  Video01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  sideDrawerContentClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { StatusBadge } from '@/components/status-badge'
import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Progress } from '@/components/ui/progress'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { formatTimestampToDate } from '@/lib/format'
import { api } from '@/lib/http-client'

import { TASK_ACTIONS, TASK_STATUS } from '../../constants'
import {
  taskActionMapper,
  taskPlatformMapper,
  taskStatusMapper,
} from '../../lib/mappers'
import type { TaskLog } from '../../types'

const VIDEO_ACTIONS = new Set<string>([
  TASK_ACTIONS.GENERATE,
  TASK_ACTIONS.TEXT_GENERATE,
  TASK_ACTIONS.FIRST_TAIL_GENERATE,
  TASK_ACTIONS.REFERENCE_GENERATE,
  TASK_ACTIONS.REMIX_GENERATE,
])

function parseJsonValue(value: unknown): unknown {
  if (typeof value !== 'string') return value
  const trimmed = value.trim()
  if (!trimmed) return value
  try {
    return JSON.parse(trimmed)
  } catch {
    return value
  }
}

function getTaskProgressValue(log: TaskLog): number | null {
  if (log.status === TASK_STATUS.SUCCESS) return 100
  const parsed = Number.parseFloat((log.progress ?? '').replace('%', ''))
  if (!Number.isFinite(parsed)) return null
  return Math.min(100, Math.max(0, parsed))
}

function buildPublicTaskData(log: TaskLog): Record<string, unknown> {
  return {
    id: log.id,
    task_id: log.task_id,
    platform: log.platform,
    user_id: log.user_id,
    username: log.username,
    group: log.group,
    channel_id: log.channel_id,
    quota: log.quota,
    action: log.action,
    status: log.status,
    fail_reason: log.fail_reason ?? '',
    result_url: log.result_url,
    submit_time: log.submit_time,
    start_time: log.start_time,
    finish_time: log.finish_time,
    progress: log.progress,
    progress_message_en: log.progress_message_en,
    properties: parseJsonValue(log.properties),
    data: parseJsonValue(log.data),
    created_at: log.created_at,
    updated_at: log.updated_at,
  }
}

function formatTaskTime(value?: number): string {
  return value ? formatTimestampToDate(value, 'seconds') : '-'
}

function DetailItem(props: { label: string; value: React.ReactNode }) {
  return (
    <div className='min-w-0'>
      <dt className='text-muted-foreground text-xs leading-5'>{props.label}</dt>
      <dd className='mt-0.5 truncate text-sm font-medium'>
        {props.value || '-'}
      </dd>
    </div>
  )
}

function SectionTitle(props: {
  icon: Parameters<typeof HugeiconsIcon>[0]['icon']
  title: string
  description?: string
  actions?: React.ReactNode
}) {
  return (
    <div className='flex items-start justify-between gap-3'>
      <div className='flex min-w-0 items-start gap-2.5'>
        <div className='bg-muted text-muted-foreground mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-lg'>
          <HugeiconsIcon icon={props.icon} className='size-4' strokeWidth={2} />
        </div>
        <div className='min-w-0'>
          <h3 className='text-sm font-semibold tracking-tight'>
            {props.title}
          </h3>
          {props.description ? (
            <p className='text-muted-foreground mt-0.5 text-xs leading-5'>
              {props.description}
            </p>
          ) : null}
        </div>
      </div>
      {props.actions ? (
        <div className='flex shrink-0 items-center gap-1.5'>
          {props.actions}
        </div>
      ) : null}
    </div>
  )
}

export function TaskDetailsSheet(props: { log: TaskLog }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [previewUrl, setPreviewUrl] = useState<string | null>(null)
  const [previewLoading, setPreviewLoading] = useState(false)
  const [previewError, setPreviewError] = useState(false)
  const previewUrlRef = useRef<string | null>(null)
  const requestControllerRef = useRef<AbortController | null>(null)
  const { copiedText, copyToClipboard } = useCopyToClipboard({ notify: false })

  const log = props.log
  const failReason = log.fail_reason?.trim() ?? ''
  const hasLegacyResultUrl =
    failReason.startsWith('http') || failReason.startsWith('data:')
  const resultUrl =
    log.result_url?.trim() || (hasLegacyResultUrl ? failReason : '')
  const visibleFailReason = hasLegacyResultUrl ? '' : failReason
  const isVideoTask = VIDEO_ACTIONS.has(log.action)
  const canPreviewVideo =
    log.status === TASK_STATUS.SUCCESS &&
    isVideoTask &&
    Boolean(resultUrl && log.task_id)
  const progressValue = getTaskProgressValue(log)
  const publicTaskJson = useMemo(
    () => JSON.stringify(buildPublicTaskData(log), null, 2),
    [log]
  )

  const releasePreview = () => {
    requestControllerRef.current?.abort()
    requestControllerRef.current = null
    if (previewUrlRef.current) {
      URL.revokeObjectURL(previewUrlRef.current)
      previewUrlRef.current = null
    }
    setPreviewUrl(null)
    setPreviewLoading(false)
    setPreviewError(false)
  }

  const loadVideoPreview = async () => {
    if (!canPreviewVideo || previewLoading || previewUrlRef.current) return

    const controller = new AbortController()
    requestControllerRef.current = controller
    setPreviewLoading(true)
    setPreviewError(false)

    try {
      const response = await api.get(
        `/v1/videos/${encodeURIComponent(log.task_id)}/content`,
        {
          responseType: 'blob',
          signal: controller.signal,
          disableDuplicate: true,
          skipErrorHandler: true,
        }
      )
      if (controller.signal.aborted) return
      if (!(response.data instanceof Blob) || response.data.size === 0) {
        throw new Error('empty video result')
      }
      const objectUrl = URL.createObjectURL(response.data)
      previewUrlRef.current = objectUrl
      setPreviewUrl(objectUrl)
    } catch {
      if (!controller.signal.aborted) setPreviewError(true)
    } finally {
      if (requestControllerRef.current === controller) {
        requestControllerRef.current = null
      }
      if (!controller.signal.aborted) setPreviewLoading(false)
    }
  }

  const handleOpenChange = (nextOpen: boolean) => {
    setOpen(nextOpen)
    if (nextOpen) {
      void loadVideoPreview()
      return
    }
    releasePreview()
  }

  useEffect(() => {
    return () => {
      requestControllerRef.current?.abort()
      if (previewUrlRef.current) URL.revokeObjectURL(previewUrlRef.current)
    }
  }, [])

  const statusLabel = t(
    taskStatusMapper.getLabel(log.status, log.status || 'Unknown')
  )
  const actionLabel = t(
    taskActionMapper.getLabel(log.action, log.action || 'Unknown')
  )
  const platformLabel = t(
    taskPlatformMapper.getLabel(log.platform, log.platform || 'Unknown')
  )

  let previewContent: React.ReactNode = (
    <Empty className='bg-muted/10 min-h-40 border'>
      <EmptyHeader>
        <EmptyMedia variant='icon'>
          <HugeiconsIcon icon={Video01Icon} />
        </EmptyMedia>
        <EmptyTitle>{t('No result is available yet')}</EmptyTitle>
        <EmptyDescription>
          {t('The preview will be available after the task succeeds.')}
        </EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
  if (previewLoading) {
    previewContent = (
      <div className='relative aspect-video overflow-hidden rounded-xl border'>
        <Skeleton className='size-full rounded-none' />
        <div className='text-muted-foreground absolute inset-0 flex items-center justify-center text-sm'>
          {t('Loading...')}
        </div>
      </div>
    )
  } else if (previewError) {
    previewContent = (
      <Alert variant='destructive'>
        <HugeiconsIcon icon={Alert02Icon} />
        <AlertTitle>{t('Video preview could not be loaded')}</AlertTitle>
        <AlertDescription>
          {t('You can retry after checking the task result.')}
        </AlertDescription>
        <AlertAction>
          <Button
            variant='outline'
            size='sm'
            onClick={() => void loadVideoPreview()}
          >
            <HugeiconsIcon icon={RefreshIcon} data-icon='inline-start' />
            {t('Retry')}
          </Button>
        </AlertAction>
      </Alert>
    )
  } else if (previewUrl) {
    previewContent = (
      <div className='bg-muted/20 aspect-video overflow-hidden rounded-xl border'>
        <video
          src={previewUrl}
          className='size-full bg-black object-contain'
          controls
          playsInline
          preload='metadata'
          aria-label={t('Video result')}
        />
      </div>
    )
  }

  return (
    <Sheet open={open} onOpenChange={handleOpenChange}>
      <SheetTrigger
        render={
          <Button
            variant='link'
            size='xs'
            className='h-auto px-0 font-medium'
          />
        }
      >
        <HugeiconsIcon icon={Task01Icon} data-icon='inline-start' />
        {t('View details')}
      </SheetTrigger>

      <SheetContent
        side='right'
        className={sideDrawerContentClassName('max-w-none sm:!max-w-[760px]')}
      >
        <SheetHeader className={sideDrawerHeaderClassName('pr-12')}>
          <div className='flex flex-wrap items-center gap-2'>
            <SheetTitle className='text-lg font-semibold tracking-tight'>
              {t('Task Details')}
            </SheetTitle>
            <StatusBadge
              label={statusLabel}
              variant={taskStatusMapper.getVariant(log.status)}
              size='sm'
              copyable={false}
            />
          </div>
          <SheetDescription className='flex flex-wrap items-center gap-x-2 gap-y-1'>
            <span>{t('Task information and generated result')}</span>
            <span aria-hidden='true'>·</span>
            <span className='font-mono text-xs'>{log.task_id || '-'}</span>
          </SheetDescription>
        </SheetHeader>

        <ScrollArea className='min-h-0 flex-1'>
          <div className='flex flex-col gap-6 px-4 py-5 sm:px-6'>
            <section aria-label={t('Task Details')}>
              <dl className='grid grid-cols-2 gap-x-5 gap-y-4 sm:grid-cols-3'>
                <DetailItem label={t('Action')} value={actionLabel} />
                <DetailItem
                  label={t('Progress')}
                  value={log.progress || (progressValue === 100 ? '100%' : '-')}
                />
                <DetailItem
                  label={t('User')}
                  value={log.username || String(log.user_id || '-')}
                />
                <DetailItem label={t('Platform')} value={platformLabel} />
                <DetailItem
                  label={t('Submit Time')}
                  value={formatTaskTime(log.submit_time)}
                />
                <DetailItem
                  label={t('Start Time')}
                  value={formatTaskTime(log.start_time)}
                />
                <DetailItem
                  label={t('Finish Time')}
                  value={formatTaskTime(log.finish_time)}
                />
                <DetailItem
                  label={t('Channel')}
                  value={log.channel_id ? `#${log.channel_id}` : '-'}
                />
                <DetailItem label={t('Group')} value={log.group || '-'} />
              </dl>

              {progressValue !== null && log.status !== TASK_STATUS.SUCCESS ? (
                <div className='mt-5'>
                  <Progress value={progressValue} aria-label={t('Progress')} />
                  {log.progress_message_en ? (
                    <p className='text-muted-foreground mt-2 text-xs leading-5'>
                      {log.progress_message_en}
                    </p>
                  ) : null}
                </div>
              ) : null}
            </section>

            {visibleFailReason ? (
              <Alert variant='destructive'>
                <HugeiconsIcon icon={Alert02Icon} />
                <AlertTitle>{t('Task failed')}</AlertTitle>
                <AlertDescription className='wrap-break-word whitespace-pre-wrap'>
                  {visibleFailReason}
                </AlertDescription>
                <AlertAction>
                  <Button
                    variant='ghost'
                    size='icon-sm'
                    aria-label={t('Copy to clipboard')}
                    onClick={() => copyToClipboard(visibleFailReason)}
                  >
                    <HugeiconsIcon
                      icon={
                        copiedText === visibleFailReason
                          ? CheckmarkCircle02Icon
                          : Copy01Icon
                      }
                    />
                  </Button>
                </AlertAction>
              </Alert>
            ) : null}

            <Separator />

            <section
              className='flex flex-col gap-3'
              aria-label={t('Result Preview')}
            >
              <SectionTitle
                icon={Video01Icon}
                title={t('Result Preview')}
                description={canPreviewVideo ? t('Video result') : undefined}
                actions={
                  canPreviewVideo ? (
                    <>
                      <Button
                        variant='outline'
                        size='sm'
                        disabled={!previewUrl}
                        onClick={() => {
                          if (previewUrl) {
                            window.open(
                              previewUrl,
                              '_blank',
                              'noopener,noreferrer'
                            )
                          }
                        }}
                      >
                        <HugeiconsIcon
                          icon={ArrowUpRight01Icon}
                          data-icon='inline-start'
                        />
                        {t('Open result')}
                      </Button>
                      {resultUrl ? (
                        <Button
                          variant='ghost'
                          size='icon-sm'
                          aria-label={t('Copy result URL')}
                          onClick={() => copyToClipboard(resultUrl)}
                        >
                          <HugeiconsIcon
                            icon={
                              copiedText === resultUrl
                                ? CheckmarkCircle02Icon
                                : Copy01Icon
                            }
                          />
                        </Button>
                      ) : null}
                    </>
                  ) : undefined
                }
              />

              {previewContent}
            </section>

            <Separator />

            <section
              className='flex flex-col gap-3'
              aria-label={t('Public task data')}
            >
              <SectionTitle
                icon={CodeIcon}
                title={t('Public task data')}
                actions={
                  <Button
                    variant='ghost'
                    size='icon-sm'
                    aria-label={t('Copy task data')}
                    onClick={() => copyToClipboard(publicTaskJson)}
                  >
                    <HugeiconsIcon
                      icon={
                        copiedText === publicTaskJson
                          ? CheckmarkCircle02Icon
                          : Copy01Icon
                      }
                    />
                  </Button>
                }
              />
              <pre className='bg-muted/30 max-h-80 overflow-auto rounded-xl border p-4 font-mono text-xs leading-5 whitespace-pre'>
                {publicTaskJson}
              </pre>
            </section>
          </div>
        </ScrollArea>
      </SheetContent>
    </Sheet>
  )
}
