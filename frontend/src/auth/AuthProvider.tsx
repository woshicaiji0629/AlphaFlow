import { createContext, useContext, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getCurrentUser, login, logout } from '../api/client'
import type { AuthUser } from '../api/types'

interface AuthContextValue {
  user: AuthUser
  signOut: () => Promise<void>
  signingOut: boolean
}

const AuthContext = createContext<AuthContextValue | null>(null)
export const authQueryKey = ['auth', 'me'] as const

export function useSessionQuery() {
  return useQuery({ queryKey: authQueryKey, queryFn: ({ signal }) => getCurrentUser(signal), retry: false, staleTime: 60_000 })
}

export function AuthProvider({ user, children }: { user: AuthUser; children: ReactNode }) {
  const queryClient = useQueryClient()
  const mutation = useMutation({ mutationFn: logout, onSuccess: () => queryClient.setQueryData(authQueryKey, null) })
  return <AuthContext.Provider value={{ user, signOut: mutation.mutateAsync, signingOut: mutation.isPending }}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const value = useContext(AuthContext)
  if (!value) throw new Error('useAuth must be used inside AuthProvider')
  return value
}

export function useLogin() {
  const queryClient = useQueryClient()
  return useMutation({ mutationFn: ({ email, password }: { email: string; password: string }) => login(email, password), onSuccess: (data) => queryClient.setQueryData(authQueryKey, data) })
}
