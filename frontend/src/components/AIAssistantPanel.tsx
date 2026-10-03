import { useCallback, useEffect, useRef, useState } from 'react'
import { Badge, Button, Drawer, Input, List, message } from 'antd'
import { RobotOutlined, SendOutlined } from '@ant-design/icons'
import { assistantApi } from '../api/assistant'

// AI 智能助理对话面板（工作台与管理控制台共用，OPIC-SSO-01/模型配置共用了同一 /api/assistant/chat）。
// 用法: <AIAssistantPanel open onClose />（容器内抽屉承载，由调用方控制开关）。
interface ChatMsg {
  id: number
  role: 'user' | 'assistant'
  text: string
  action?: string
}

export default function AIAssistantPanel({
  open,
  onClose,
}: {
  open: boolean
  onClose: () => void
}) {
  const [messages, setMessages] = useState<ChatMsg[]>([])
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [hints, setHints] = useState<string[]>([
    '我有哪些待办？',
    '我的客户申请进展？',
    '审批怎么操作？',
    '流程有哪些节点？',
  ])
  const listRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (listRef.current) {
      listRef.current.scrollTop = listRef.current.scrollHeight
    }
  }, [messages])

  const send = useCallback(
    async (preset?: string) => {
      const q = (preset ?? input).trim()
      if (!q || busy) return
      setInput('')
      setMessages((m) => [...m, { id: Date.now(), role: 'user', text: q }])
      setBusy(true)
      try {
        const reply = await assistantApi.chat(q)
        setMessages((m) => [
          ...m,
          { id: Date.now() + 1, role: 'assistant', text: reply.answer, action: reply.action },
        ])
        if (reply.hints?.length) setHints(reply.hints)
      } catch {
        setMessages((m) => [
          ...m,
          { id: Date.now() + 1, role: 'assistant', text: '请求失败，请稍后重试' },
        ])
      } finally {
        setBusy(false)
      }
    },
    [busy, input],
  )

  return (
    <Drawer
      title={
        <span>
          <RobotOutlined style={{ color: '#2266e3', marginRight: 8 }} />
          AI 智能助理
        </span>
      }
      placement="right"
      width={420}
      open={open}
      onClose={onClose}
      styles={{ body: { padding: 0, display: 'flex', flexDirection: 'column' } }}
      footer={
        <div style={{ display: 'flex', gap: 8 }}>
          <Input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onPressEnter={() => send()}
            placeholder="输入问题，如：我有哪些待办？"
            disabled={busy}
          />
          <Button type="primary" loading={busy} onClick={() => send()}>
            发送
          </Button>
        </div>
      }
    >
      <div
        ref={listRef}
        style={{ flex: 1, overflowY: 'auto', padding: '12px 16px', background: '#f7f9fc' }}
      >
        {messages.length === 0 && (
          <div style={{ textAlign: 'center', color: '#999', padding: '24px 8px' }}>
            <RobotOutlined style={{ fontSize: 32, marginBottom: 12 }} />
            <div>我是 OPIC AI 智能助理，试试下方建议问题</div>
          </div>
        )}
        <List
          dataSource={messages}
          renderItem={(m) => (
            <List.Item
              style={{
                justifyContent: m.role === 'user' ? 'flex-end' : 'flex-start',
                display: 'flex',
                padding: '4px 0',
                border: 'none',
              }}
            >
              <div
                style={{
                  maxWidth: '85%',
                  padding: '8px 12px',
                  borderRadius: 10,
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-word',
                  background: m.role === 'user' ? '#2266e3' : '#fff',
                  color: m.role === 'user' ? '#fff' : 'inherit',
                  border: m.role === 'assistant' ? '1px solid #e5e6eb' : 'none',
                }}
              >
                {m.text}
              </div>
            </List.Item>
          )}
        />
        {busy && (
          <div style={{ textAlign: 'center', color: '#999' }}>
            <Badge status="processing" /> 思考中…
          </div>
        )}
      </div>
      <div style={{ padding: '8px 12px', borderTop: '1px solid #f0f0f0' }}>
        {hints.map((h) => (
          <Button
            key={h}
            size="small"
            type="link"
            style={{ padding: '0 6px' }}
            onClick={() => send(h)}
          >
            {h}
          </Button>
        ))}
      </div>
    </Drawer>
  )
}
