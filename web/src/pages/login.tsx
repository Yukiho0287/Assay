import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { KeyRound, LogIn } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { authApi, RequestError } from '@/lib/api'
import { useI18n } from '@/lib/i18n'

export default function LoginPage() {
  const navigate = useNavigate()
  const { t } = useI18n()
  const [searchParams, setSearchParams] = useSearchParams()
  const [formError, setFormError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)
  const [passwordOpen, setPasswordOpen] = useState(false)

  // 飞书回调失败会跳回 /login?error=…；渲染期直接取，不用 effect 同步 state
  const error = formError ?? searchParams.get('error')

  // 登录方式由后端配置决定：没配飞书就只出密码表单，配了就以飞书为主入口
  const methods = useQuery({ queryKey: ['authMethods'], queryFn: authApi.methods })
  const feishuEnabled = methods.data?.feishu ?? false
  const passwordEnabled = methods.data?.password ?? true

  // 上一次失败的提示不该盖住这一次操作的结果，动手前先把它从地址栏抹掉
  function clearCallbackError() {
    if (!searchParams.has('error')) return
    searchParams.delete('error')
    setSearchParams(searchParams, { replace: true })
  }

  async function handleSubmit(e: React.SubmitEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    setPending(true)
    setFormError(null)
    clearCallbackError()
    try {
      await authApi.login(String(form.get('username') ?? ''), String(form.get('password') ?? ''))
      navigate('/', { replace: true })
    } catch (err) {
      setFormError(err instanceof RequestError ? err.message : t('login.networkError'))
    } finally {
      setPending(false)
    }
  }

  // 只有密码一种方式时不必折叠，直接展开
  const showPasswordForm = passwordEnabled && (!feishuEnabled || passwordOpen)

  return (
    <div className="flex min-h-svh items-center justify-center bg-muted/40 p-6">
      <Card className="w-full max-w-sm">
        <CardHeader className="items-center text-center">
          <img src="/logo.png" alt="Assay" className="mx-auto mb-2 size-16 rounded-2xl" />
          <CardTitle className="text-xl">Assay</CardTitle>
          <CardDescription>{t('login.desc')}</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          {error && <p className="text-sm text-destructive">{error}</p>}

          {feishuEnabled && (
            <Button
              className="w-full"
              onClick={() => {
                clearCallbackError()
                window.location.assign(authApi.feishuLoginUrl)
              }}
            >
              <LogIn />
              {t('login.feishu')}
            </Button>
          )}

          {feishuEnabled && passwordEnabled && (
            <div className="flex items-center gap-3">
              <span className="h-px flex-1 bg-border" />
              <span className="text-xs text-muted-foreground">{t('login.or')}</span>
              <span className="h-px flex-1 bg-border" />
            </div>
          )}

          {feishuEnabled && passwordEnabled && (
            <Button
              variant="ghost"
              size="sm"
              className="w-full text-muted-foreground"
              onClick={() => setPasswordOpen((v) => !v)}
            >
              <KeyRound />
              {passwordOpen ? t('login.hidePassword') : t('login.usePassword')}
            </Button>
          )}

          {showPasswordForm && (
            <form className="grid gap-4" onSubmit={handleSubmit}>
              {feishuEnabled && (
                <p className="text-xs text-muted-foreground">{t('login.passwordHint')}</p>
              )}
              <div className="grid gap-2">
                <Label htmlFor="username">{t('login.username')}</Label>
                <Input id="username" name="username" autoComplete="username" required />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="password">{t('login.password')}</Label>
                <Input
                  id="password"
                  name="password"
                  type="password"
                  autoComplete="current-password"
                  required
                />
              </div>
              <Button type="submit" variant={feishuEnabled ? 'outline' : 'default'} className="w-full" disabled={pending}>
                {pending ? t('login.pending') : t('login.submit')}
              </Button>
            </form>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
