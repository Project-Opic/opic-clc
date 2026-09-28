import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Lock, LogIn, User } from 'lucide-react'
import { authApi } from '../api/auth'
import { setToken, clearToken } from '../api/request'
import { useAuth } from '../stores/userStore'
import { toast } from '../components/toast'

export default function Login() {
  const navigate = useNavigate()
  const { login } = useAuth()
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('admin123')
  const [loading, setLoading] = useState(false)
  const [zitadelMode, setZitadelMode] = useState(false)

  // ZITADEL 回调:授权码流程完成后服务端重定向回 /login#token=<会话令牌>
  useEffect(() => {
    const hash = window.location.hash || ''
    const m = hash.match(/[#&]token=([^&]+)/)
    if (m) {
      history.replaceState(null, '', window.location.pathname)
      setToken(m[1])
      authApi
        .info()
        .then((info) => {
          login({ ...info, token: m[1] })
          toast('ZITADEL 登录成功')
          navigate('/')
        })
        .catch(() => {
          clearToken()
          toast('ZITADEL 回调换取会话失败', 'error')
        })
      return
    }
    authApi.mode().then((r) => setZitadelMode(r.mode === 'zitadel')).catch(() => {})
  }, [login, navigate])

  const handleZitadel = async () => {
    try {
      const { url } = await authApi.zitadelLoginUrl()
      window.location.href = url
    } catch (err: any) {
      toast(err.message || '跳转 ZITADEL 失败', 'error')
    }
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!username || !password) {
      toast('请输入用户名和密码', 'error')
      return
    }
    setLoading(true)
    try {
      const payload = await authApi.login(username, password)
      login(payload)
      toast('登录成功')
      navigate('/')
    } catch (err: any) {
      toast(err.message || '登录失败', 'error')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-wrap">
      <form className="login-card" onSubmit={handleSubmit}>
        <div style={{ textAlign: 'center' }}>
          <div className="login-logo">
            <Lock size={32} />
          </div>
          <h1 className="login-title">客户管理技术底座</h1>
          <p className="login-subtitle">请登录您的账号以继续</p>
        </div>
        <div className="input-with-icon">
          <User size={16} />
          <input
            type="text"
            placeholder="用户名"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
          />
        </div>
        <div className="input-with-icon">
          <Lock size={16} />
          <input
            type="password"
            placeholder="密码"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
          />
        </div>
        <button type="submit" className="btn btn-primary" style={{ width: '100%', height: '44px' }} disabled={loading}>
          <LogIn size={16} /> {loading ? '登录中…' : '立即登录'}
        </button>
        <button
          type="button"
          className="btn"
          style={{ width: '100%', height: '44px', marginTop: '12px', border: '1px solid var(--border-color, #e2edf2)', background: '#fff', color: '#2266e3' }}
          onClick={handleZitadel}
        >
          使用 ZITADEL 单点登录
        </button>
        <div className="login-hint">默认管理员账号：admin / admin123{zitadelMode ? '；已接入 ZITADEL(零信任中心)' : ''}</div>
      </form>
    </div>
  )
}
