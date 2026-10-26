import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router'
import { api, setUnauthorizedHandler, type User } from './api'

type AuthState = {
  user: User | null
  loading: boolean
  refresh: () => Promise<void>
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)
  const navigate = useNavigate()

  const refresh = useCallback(async () => {
    try {
      const res = await api.get<{ user: User }>('/me')
      setUser(res.user)
    } catch {
      setUser(null)
    } finally {
      setLoading(false)
    }
  }, [])

  const logout = useCallback(async () => {
    await api.post('/auth/logout').catch(() => {})
    setUser(null)
    navigate('/login')
  }, [navigate])

  useEffect(() => {
    setUnauthorizedHandler(() => {
      setUser(null)
      navigate('/login')
    })
    void refresh()
  }, [navigate, refresh])

  return <AuthContext.Provider value={{ user, loading, refresh, logout }}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth outside AuthProvider')
  return ctx
}

// Role permissions mirror internal/auth/roles.go for hiding UI the server
// would refuse anyway.
const perms: Record<string, string[]> = {
  owner: ['*'],
  admin: ['customers.write', 'licenses.write', 'licenses.revoke', 'activation.offline', 'installations.write', 'resellers.write', 'audit.read'],
  support: ['activation.offline', 'installations.write'],
  publisher: ['repo.publish'],
  viewer: [],
}

export function can(user: User | null, perm: string): boolean {
  const p = user ? perms[user.role] ?? [] : []
  return p.includes('*') || p.includes(perm)
}
