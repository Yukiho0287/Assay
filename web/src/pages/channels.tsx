import { useI18n } from '@/lib/i18n'

export default function ChannelsPage() {
  const { t } = useI18n()
  return (
    <div>
      <h1 className="text-2xl font-semibold">{t('nav.channels')}</h1>
      <p className="mt-1 text-sm text-muted-foreground">{t('channels.desc')}</p>
    </div>
  )
}
