import { Navigate, Outlet, useLocation } from 'react-router-dom'
import { APIError } from '../api/client'
import { AuthProvider, useSessionQuery } from './AuthProvider'

export function ProtectedRoute() {
  const location = useLocation()
  const session = useSessionQuery()
  if (session.isPending) return <main className="state-page"><span className="loader"/><p>正在验证登录状态…</p></main>
  if (session.error instanceof APIError && session.error.status === 401) return <Navigate to="/login" replace state={{ from: location }} />
  if (session.isError) return <main className="state-page"><p>暂时无法验证登录状态</p><small>{session.error.message}</small></main>
  if (!session.data?.user) return <Navigate to="/login" replace />
  return <AuthProvider user={session.data.user}><Outlet /></AuthProvider>
}
