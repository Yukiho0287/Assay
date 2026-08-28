import { Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import {
  RequestError,
  type Channel,
  type ChannelCreate,
  type ConnectivityTest,
  type Currency,
  type Protocol,
} from '@/lib/api'
import { type DictKey, useI18n } from '@/lib/i18n'

export const protocolKeys = ['openai_chat', 'openai_responses', 'anthropic_messages'] as const

export function errText(err: unknown): string {
  return err instanceof RequestError ? err.message : String(err)
}

// ConnStatus 连通状态点：绿=全部协议通过，黄=部分，红=全挂，灰=未测试
export function ConnStatus({ test }: { test?: ConnectivityTest }) {
  const { t } = useI18n()
  if (!test) {
    return <span className="text-xs text-muted-foreground">{t('channels.untested')}</span>
  }
  const ok = test.results.filter((r) => r.ok).length
  const total = test.results.length
  const color = ok === total ? 'bg-green-500' : ok === 0 ? 'bg-red-500' : 'bg-amber-500'
  return (
    <span className="inline-flex items-center gap-1.5 text-xs" title={new Date(test.testedAt).toLocaleString()}>
      <span className={`size-2 rounded-full ${color}`} />
      {ok}/{total}
    </span>
  )
}

// ChannelFormDialog 新建/编辑渠道共用表单；编辑时 apiKey 留空表示不修改
export function ChannelFormDialog({
  open,
  title,
  initial,
  pending,
  error,
  onClose,
  onSubmit,
}: {
  open: boolean
  title: string
  initial?: Channel
  pending: boolean
  error: unknown
  onClose: () => void
  onSubmit: (v: ChannelCreate) => void
}) {
  const { t } = useI18n()

  function handleSubmit(e: React.SubmitEvent<HTMLFormElement>) {
    e.preventDefault()
    const data = new FormData(e.currentTarget)
    onSubmit({
      name: String(data.get('name') ?? ''),
      baseUrl: String(data.get('baseUrl') ?? ''),
      apiKey: String(data.get('apiKey') ?? ''),
      protocols: protocolKeys.filter((k) => data.get(`proto-${k}`) === 'on') as Protocol[],
      currency: String(data.get('currency') ?? 'USD') as Currency,
      note: String(data.get('note') ?? ''),
    })
  }

  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) onClose() }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        {/* key 强制在切换编辑对象时重置非受控表单默认值 */}
        <form key={initial?.id ?? 'new'} className="grid gap-4" onSubmit={handleSubmit}>
          <div className="grid gap-2">
            <Label htmlFor="ch-name">{t('channels.name')}</Label>
            <Input id="ch-name" name="name" maxLength={64} defaultValue={initial?.name} required />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="ch-baseUrl">{t('channels.baseUrl')}</Label>
            <Input
              id="ch-baseUrl"
              name="baseUrl"
              type="url"
              placeholder="https://api.example.com/v1"
              maxLength={512}
              defaultValue={initial?.baseUrl}
              required
            />
            <p className="text-xs text-muted-foreground">{t('channels.baseUrlHint')}</p>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="ch-apiKey">
              {initial ? t('channels.apiKeyEditHint') : t('channels.apiKey')}
            </Label>
            <Input
              id="ch-apiKey"
              name="apiKey"
              type="password"
              autoComplete="off"
              placeholder={initial ? initial.keyPrefix : undefined}
              required={!initial}
            />
            {!initial && <p className="text-xs text-muted-foreground">{t('channels.apiKeyHint')}</p>}
          </div>
          <div className="grid gap-3">
            <Label>{t('channels.protocols')}</Label>
            {protocolKeys.map((k) => (
              <div key={k} className="flex items-center justify-between">
                <span className="text-sm">{t(`proto.${k}` as DictKey)}</span>
                <Switch
                  name={`proto-${k}`}
                  defaultChecked={initial ? initial.protocols.includes(k) : k === 'openai_chat'}
                />
              </div>
            ))}
          </div>
          <div className="grid gap-2">
            <Label>{t('channels.currency')}</Label>
            <Select name="currency" defaultValue={initial?.currency ?? 'USD'}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="USD">USD</SelectItem>
                <SelectItem value="CNY">CNY</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="ch-note">{t('channels.note')}</Label>
            <Input id="ch-note" name="note" maxLength={500} defaultValue={initial?.note} />
          </div>
          {error != null && <p className="text-sm text-destructive">{errText(error)}</p>}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={pending}>
              {pending && <Loader2 className="animate-spin" />}
              {initial ? t('common.save') : t('common.create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
