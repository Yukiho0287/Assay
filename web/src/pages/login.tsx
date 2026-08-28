import { useState } from 'react'
import { useNavigate } from 'react-router'
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

export default function LoginPage() {
  const navigate = useNavigate()
  const [error, setError] = useState<string | null>(null)
  const [pending, setPending] = useState(false)

  async function handleSubmit(e: React.SubmitEvent<HTMLFormElement>) {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    setPending(true)
    setError(null)
    try {
      await authApi.login(String(form.get('username') ?? ''), String(form.get('password') ?? ''))
      navigate('/', { replace: true })
    } catch (err) {
      setError(err instanceof RequestError ? err.message : '网络错误，请重试')
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="flex min-h-svh items-center justify-center bg-muted/40 p-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="text-xl">Assay</CardTitle>
          <CardDescription>LLM 渠道检测平台，请登录</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="grid gap-4" onSubmit={handleSubmit}>
            <div className="grid gap-2">
              <Label htmlFor="username">用户名</Label>
              <Input id="username" name="username" autoComplete="username" required />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="password">密码</Label>
              <Input
                id="password"
                name="password"
                type="password"
                autoComplete="current-password"
                required
              />
            </div>
            {error && <p className="text-sm text-destructive">{error}</p>}
            <Button type="submit" className="w-full" disabled={pending}>
              {pending ? '登录中…' : '登录'}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
