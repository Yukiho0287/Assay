import { useI18n } from '@/lib/i18n'

export default function QualityPage() {
  const { t } = useI18n()
  return (
    <div>
      <h1 className="text-2xl font-semibold">{t('nav.quality')}</h1>
      <p className="mt-1 text-sm text-muted-foreground">{t('quality.desc')}</p>
    </div>
  )
}
