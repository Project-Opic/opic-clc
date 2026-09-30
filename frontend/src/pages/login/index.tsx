// 视觉体系移植自 gopherforge (https://github.com/SuperiorChuo/gopherforge) MIT License,适配 OPIC 技术底座。
// 全新玻璃登录页(深空极光 + 920px 玻璃壳左右分栏)。
// 登录方式显示矩阵(mode × allow_local_login):
//   zitadel + allow_local_login=true  → 密码表单 + SSO 按钮
//   zitadel + allow_local_login=false → 仅 SSO 按钮
//   local    + allow_local_login=true → 仅密码表单(维持现状)
//   local    + allow_local_login=false → 两者都无,提示联系管理员
//   /auth/mode 检测失败              → 按旧默认显示密码表单(部署环境 AUTH_MODE=local)
// 另:#token= hash 恢复、登录成功后一律跳转用户工作台 /customer/apply。
import { useEffect, useState } from 'react'
import { useDispatch } from 'react-redux'
import { useNavigate } from 'react-router-dom'
import { Button, Form, Input, message } from 'antd'
import {
  LockOutlined,
  UserOutlined,
  SafetyCertificateOutlined,
  ThunderboltOutlined,
  ApartmentOutlined,
  InfoCircleOutlined,
} from '@ant-design/icons'
import { authApi } from '../../api/auth'
import { setToken, clearToken } from '../../api/request'
import { setAuth } from '../../store/slices/authSlice'
import type { AppDispatch } from '../../store'
import { resolveHomePath } from '../../router'

export default function Login() {
  const navigate = useNavigate()
  const dispatch = useDispatch<AppDispatch>()
  const [loading, setLoading] = useState(false)
  const [ssoMode, setSsoMode] = useState(false)
  /** allow_local_login;null = /auth/mode 检测失败(兜底显示密码表单,维持旧行为) */
  const [localAllowed, setLocalAllowed] = useState<boolean | null>(null)

  // mode=zitadel 时始终展示 SSO;密码表单仅在 allow_local_login=true 时展示
  const showSso = ssoMode
  const showLocal = ssoMode ? localAllowed === true : localAllowed !== false

  useEffect(() => {
    // ZITADEL 回调:授权码流程完成后服务端重定向回 /login#token=<会话令牌>
    const hash = window.location.hash || ''
    const m = hash.match(/[#&]token=([^&]+)/)
    if (m) {
      history.replaceState(null, '', window.location.pathname)
      const token = decodeURIComponent(m[1])
      setToken(token)
      authApi
        .info()
        .then((info) => {
          dispatch(setAuth({ token, info }))
          message.success('登录成功')
          navigate(resolveHomePath(info.permissions), { replace: true })
        })
        .catch(() => {
          clearToken()
          message.error('ZITADEL 回调换取会话失败')
        })
      return
    }
    authApi
      .mode()
      .then((r) => {
        setSsoMode(r.mode === 'zitadel')
        setLocalAllowed(!!r.allow_local_login)
      })
      .catch(() => {
        // 检测失败:维持旧行为,显示本地密码表单
        setLocalAllowed(null)
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const handleSso = async () => {
    try {
      const { url } = await authApi.zitadelLoginUrl()
      window.location.href = url
    } catch (err: any) {
      message.error(err.message || '跳转 ZITADEL 失败')
    }
  }

  const onFinish = async (values: { username: string; password: string }) => {
    setLoading(true)
    try {
      const payload = await authApi.login(values.username, values.password)
      setToken(payload.token)
      dispatch(setAuth({ token: payload.token, info: payload }))
      message.success('登录成功')
      navigate(resolveHomePath(payload.permissions), { replace: true })
    } catch {
      // 统一拦截器已提示
    } finally {
      setLoading(false)
    }
  }

  // 两者都无:zitadel 且不允许本地登录之外的空档(如 local + allow_local_login=false)
  if (!showSso && !showLocal) {
    return (
      <div className="brand-page">
        <div className="brand-aurora brand-aurora-1" />
        <div className="brand-aurora brand-aurora-2" />
        <div className="brand-aurora brand-aurora-3" />
        <div className="brand-grid" />

        <div className="brand-shell brand-shell-single">
          <div className="brand-form-side">
            <div className="brand-form-inner">
              <h2 className="brand-form-title">欢迎回来</h2>
              <p className="brand-form-sub">当前未开放任何登录方式</p>
              <div className="brand-notice" role="alert">
                <InfoCircleOutlined />
                <span>
                  本地密码登录已关闭,且未启用单点登录。
                  <br />
                  请联系管理员调整认证配置后再登录。
                </span>
              </div>
            </div>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="brand-page">
      <div className="brand-aurora brand-aurora-1" />
      <div className="brand-aurora brand-aurora-2" />
      <div className="brand-aurora brand-aurora-3" />
      <div className="brand-grid" />

      <div className="brand-shell">
        {/* 左品牌栏 */}
        <div className="brand-side">
          <div className="brand-logo">
            <div className="brand-logo-mark">
              <ApartmentOutlined />
            </div>
            <span className="brand-logo-name">OPIC 技术底座</span>
          </div>

          <div className="brand-brand-copy">
            <h1 className="brand-headline">
              以本体驱动,
              <br />
              筑<em>智能算力</em>底座
            </h1>
            <p className="brand-subline">
              本体建模 · 流程引擎 · 权限中台,
              <br />
              一体化支撑智能算力运营。
            </p>
          </div>

          <ul className="brand-features">
            <li>
              <span className="brand-feature-icon">
                <ApartmentOutlined />
              </span>
              本体驱动建模 · 数据与流程同源
            </li>
            <li>
              <span className="brand-feature-icon">
                <SafetyCertificateOutlined />
              </span>
              权限精密可控 · 身份统一守护
            </li>
            <li>
              <span className="brand-feature-icon">
                <ThunderboltOutlined />
              </span>
              流程引擎全速 · 每一步皆可追溯
            </li>
          </ul>
        </div>

        {/* 右表单栏 */}
        <div className="brand-form-side">
          <div className="brand-form-inner">
            {!showLocal ? (
              <>
                <h2 className="brand-form-title">欢迎回来</h2>
                <p className="brand-form-sub">请使用企业统一身份继续</p>
                <div className="brand-form">
                  <Button type="primary" block size="large" onClick={handleSso}>
                    使用 ZITADEL 单点登录
                  </Button>
                </div>
              </>
            ) : (
              <>
                <h2 className="brand-form-title">欢迎回来</h2>
                <p className="brand-form-sub">登录 OPIC 技术底座以继续</p>
                <Form
                  name="login"
                  size="large"
                  className="brand-form"
                  requiredMark={false}
                  initialValues={{ username: 'admin', password: 'admin123' }}
                  onFinish={onFinish}
                >
                  <Form.Item name="username" rules={[{ required: true, message: '请输入用户名' }]}>
                    <Input prefix={<UserOutlined />} placeholder="用户名" autoComplete="username" />
                  </Form.Item>
                  <Form.Item name="password" rules={[{ required: true, message: '请输入密码' }]}>
                    <Input.Password
                      prefix={<LockOutlined />}
                      placeholder="密码"
                      autoComplete="current-password"
                    />
                  </Form.Item>
                  <Form.Item className="brand-submit-item">
                    <Button type="primary" htmlType="submit" block loading={loading}>
                      立即登录
                    </Button>
                  </Form.Item>
                  {showSso && (
                    <Form.Item style={{ marginBottom: 0 }}>
                      <Button block onClick={handleSso}>
                        使用 ZITADEL 单点登录
                      </Button>
                    </Form.Item>
                  )}
                </Form>
                <div className="brand-footer">默认管理员账号:admin / admin123</div>
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
