import { useEffect, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { CheckCircle2, CircleAlert, Loader2, RefreshCw, Rocket } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { RequestError, systemApi } from '@/lib/api'
import { useI18n } from '@/lib/i18n'

type DeployPhase = 'idle' | 'deploying' | 'done' | 'timeout'

// 触发部署后轮询 /api/version 的间隔与上限（蓝绿切换通常 1-2 分钟内完成）
const POLL_INTERVAL_MS = 5_000
const POLL_TIMEOUT_MS = 6 * 60_000

interface UpdateDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function UpdateDialog({ open, onOpenChange }: UpdateDialogProps) {
  const { t } = useI18n()
  const [phase, setPhase] = useState<DeployPhase>('idle')

  const status = useQuery({
    queryKey: ['system', 'update'],
    queryFn: systemApi.updateStatus,
    enabled: open && phase === 'idle',
    retry: false,
    staleTime: 60_000,
  })

  const deploy = useMutation({
    mutationFn: systemApi.triggerDeploy,
    onSuccess: () => setPhase('deploying'),
  })

  const targetVersion = status.data?.latestVersion

  // 部署已触发：轮询版本号，切到目标版本即刷新页面加载新前端
  useEffect(() => {
    if (phase !== 'deploying' || !targetVersion) return
    const startedAt = Date.now()
    const timer = setInterval(async () => {
      if (Date.now() - startedAt > POLL_TIMEOUT_MS) {
        setPhase('timeout')
        return
      }
      try {
        const info = await systemApi.version()
        if (info.version === targetVersion) setPhase('done')
      } catch {
        // 蓝绿切换瞬间可能短暂失败，忽略继续轮询
      }
    }, POLL_INTERVAL_MS)
    return () => clearInterval(timer)
  }, [phase, targetVersion])

  useEffect(() => {
    if (phase !== 'done') return
    const timer = setTimeout(() => window.location.reload(), 1_500)
    return () => clearTimeout(timer)
  }, [phase])

  function handleOpenChange(next: boolean) {
    onOpenChange(next)
    // 关闭即复位（部署中关闭则停止轮询，服务端部署继续；重开可再确认版本）
    if (!next) {
      setPhase('idle')
      deploy.reset()
    }
  }

  const data = status.data

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t('update.title')}</DialogTitle>
          <DialogDescription>{t('update.desc')}</DialogDescription>
        </DialogHeader>

        {phase === 'deploying' && (
          <div className="flex items-center gap-3 rounded-md border p-4 text-sm">
            <Loader2 className="size-5 shrink-0 animate-spin text-muted-foreground" />
            <span>{t('update.deploying')}</span>
          </div>
        )}

        {phase === 'done' && (
          <div className="flex items-center gap-3 rounded-md border border-green-600/40 bg-green-600/10 p-4 text-sm">
            <CheckCircle2 className="size-5 shrink-0 text-green-600" />
            <span>{t('update.done')}</span>
          </div>
        )}

        {phase === 'timeout' && (
          <div className="flex items-center gap-3 rounded-md border border-amber-600/40 bg-amber-600/10 p-4 text-sm">
            <CircleAlert className="size-5 shrink-0 text-amber-600" />
            <span>{t('update.timeout')}</span>
          </div>
        )}

        {phase === 'idle' && (
          <div className="grid gap-4">
            {status.isPending && (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <Loader2 className="size-4 animate-spin" />
                {t('update.checking')}
              </div>
            )}

            {status.isError && (
              <p className="text-sm text-destructive">
                {status.error instanceof RequestError ? status.error.message : String(status.error)}
              </p>
            )}

            {data && (
              <>
                <div className="grid gap-1 text-sm">
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">{t('update.current')}</span>
                    <span className="font-mono">{data.currentVersion}</span>
                  </div>
                  {data.latestVersion && (
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">{t('update.latest')}</span>
                      <span className="font-mono">{data.latestVersion}</span>
                    </div>
                  )}
                  {data.publishedAt && (
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">{t('update.publishedAt')}</span>
                      <span>{new Date(data.publishedAt).toLocaleString()}</span>
                    </div>
                  )}
                </div>

                {!data.tokenConfigured ? (
                  <p className="rounded-md border border-amber-600/40 bg-amber-600/10 p-3 text-sm">
                    {t('update.noToken')}
                  </p>
                ) : data.updateAvailable ? (
                  <>
                    <p className="text-sm font-medium text-green-600">{t('update.available')}</p>
                    {data.releaseNotes && (
                      <div className="grid gap-1">
                        <span className="text-xs text-muted-foreground">{t('update.notes')}</span>
                        <pre className="max-h-48 overflow-auto whitespace-pre-wrap rounded-md bg-muted p-3 font-sans text-xs">
                          {data.releaseNotes}
                        </pre>
                      </div>
                    )}
                  </>
                ) : (
                  <p className="text-sm text-muted-foreground">{t('update.upToDate')}</p>
                )}

                {deploy.isError && (
                  <p className="text-sm text-destructive">
                    {deploy.error instanceof RequestError ? deploy.error.message : String(deploy.error)}
                  </p>
                )}

                <div className="flex justify-end gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => status.refetch()}
                    disabled={status.isFetching}
                  >
                    <RefreshCw className={status.isFetching ? 'animate-spin' : undefined} />
                    {t('update.check')}
                  </Button>
                  {data.tokenConfigured && data.updateAvailable && data.latestVersion && (
                    <Button
                      size="sm"
                      onClick={() => deploy.mutate(data.latestVersion!)}
                      disabled={deploy.isPending}
                    >
                      {deploy.isPending ? <Loader2 className="animate-spin" /> : <Rocket />}
                      {t('update.deploy')}
                    </Button>
                  )}
                </div>
              </>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
