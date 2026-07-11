import { useState, type FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { useLogin, useSessionQuery } from '../auth/AuthProvider'

export function LoginPage() {
  const session = useSessionQuery()
  const mutation = useLogin()
  const navigate = useNavigate()
  const location = useLocation()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  if (session.data?.user) return <Navigate to="/" replace />
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    await mutation.mutateAsync({ email, password })
    const from = (location.state as { from?: { pathname?: string } } | null)?.from?.pathname ?? '/'
    navigate(from, { replace: true })
  }
  return <main className="login-page"><section className="login-aside"><div className="brand login-brand"><span className="brand-mark">A</span><div><strong>ALPHAFLOW</strong><small>QUANT CONSOLE</small></div></div><div><p className="eyebrow">SYSTEM ACCESS</p><h1>在信号与噪声之间，<br/>保持绝对清醒。</h1><p>面向策略研究、回测与 Paper Trading 的统一量化工作台。</p></div><small>SECURE CONTROL PLANE · SESSION PROTECTED</small></section><section className="login-form-wrap"><form className="login-form" onSubmit={(event) => void submit(event)}><p className="eyebrow">WELCOME BACK</p><h2>登录控制台</h2><p>使用管理员邀请的账户继续。</p><label>邮箱<input type="email" autoComplete="username" value={email} onChange={(event) => setEmail(event.target.value)} required autoFocus /></label><label>密码<input type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} required minLength={12}/></label>{mutation.isError && <div className="form-error" role="alert">{mutation.error.message}</div>}<button className="submit-button" disabled={mutation.isPending}>{mutation.isPending ? '正在验证…' : '进入 AlphaFlow'}</button><small>登录行为将写入安全审计日志</small></form></section></main>
}
