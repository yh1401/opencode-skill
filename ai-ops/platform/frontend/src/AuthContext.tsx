import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';

import { api, authToken } from './api';
import type { Role, User } from './types';

interface AuthContextValue {
  user: User | null;
  loading: boolean;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  refresh: () => Promise<void>;
  hasRole: (...roles: Role[]) => boolean;
}

const AuthContext = createContext<AuthContextValue | null>(null);

/** AuthProvider 负责登录态：启动时校验本地令牌，令牌失效时自动清理。 */
export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    if (!authToken.get()) {
      setUser(null);
      setLoading(false);
      return;
    }
    try {
      setUser(await api.authMe());
    } catch {
      authToken.clear();
      setUser(null);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
    // 任意接口返回 401 时，api 层会派发该事件，这里同步清理登录态
    const onUnauthorized = () => setUser(null);
    window.addEventListener('aiops:unauthorized', onUnauthorized);
    return () => window.removeEventListener('aiops:unauthorized', onUnauthorized);
  }, [refresh]);

  const login = useCallback(async (username: string, password: string) => {
    const res = await api.authLogin(username, password);
    authToken.set(res.token);
    setUser(res.user);
    setLoading(false);
  }, []);

  const logout = useCallback(async () => {
    try {
      await api.authLogout();
    } catch {
      /* 登出接口失败不影响本地清理 */
    }
    authToken.clear();
    setUser(null);
  }, []);

  const hasRole = useCallback(
    (...roles: Role[]) => (user ? roles.includes(user.role) : false),
    [user],
  );

  const value = useMemo<AuthContextValue>(
    () => ({ user, loading, login, logout, refresh, hasRole }),
    [user, loading, login, logout, refresh, hasRole],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth 必须在 AuthProvider 内使用');
  return ctx;
}
