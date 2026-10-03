import { useCallback, useEffect, useState } from 'react'
import { Button, Form, Input, InputNumber, message, Select, Switch } from 'antd'
import { http } from '../../api/request'

// AI 模型配置页（管理控制台）：provider/base_url/model/api_key/temperature/enabled。
// 存储 opicdb.otechbase.ai_model_config（migration 000004）；api_key 回显打码，留空=保留旧值。
interface LLMCfg {
  provider: string
  base_url: string
  model: string
  api_key?: string
  temperature: number
  enabled: boolean
}

export default function AIModelConfigPage() {
  const [form] = Form.useForm()
  const [hasKey, setHasKey] = useState(false)
  const [saving, setSaving] = useState(false)

  const load = useCallback(() => {
    http
      .get<{ config: LLMCfg; has_key: boolean }>('/api/admin/llm-config')
      .then((d) => {
        setHasKey(!!d.has_key)
        form.setFieldsValue({ ...d.config, api_key: '' })
      })
      .catch((e) => message.error(e instanceof Error ? e.message : '加载失败'))
  }, [form])

  useEffect(() => {
    load()
  }, [load])

  const onSave = async () => {
    const v = await form.validateFields()
    setSaving(true)
    try {
      await http.put('/api/admin/llm-config', v)
      message.success('AI 模型配置已保存')
      setHasKey(!!v.api_key || hasKey)
      if (!v.api_key) load() // 留空保留旧密钥时刷新
    } catch (e) {
      message.error(e instanceof Error ? e.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div style={{ maxWidth: 640, padding: 16 }}>
      <h3>AI 模型配置</h3>
      <p style={{ color: '#888', fontSize: 13 }}>
        用户工作台与管理控制台的 AI 智能助理共用此配置（OpenAI 兼容协议：deepseek/qwen/vllm 等）。
        未启用时 AI 助理降级为内置规则问答。
      </p>
      <Form form={form} layout="vertical" initialValues={{ provider: 'openai', temperature: 0.3 }}>
        <Form.Item name="provider" label="协议类型" rules={[{ required: true }]}>
          <Select
            options={[
              { value: 'openai', label: 'OpenAI 兼容（deepseek/qwen/vllm/…）' },
            ]}
          />
        </Form.Item>
        <Form.Item name="base_url" label="Base URL" rules={[{ required: true }]} extra="如 https://api.deepseek.com/v1">
          <Input placeholder="https://api.deepseek.com/v1" />
        </Form.Item>
        <Form.Item name="model" label="模型名" rules={[{ required: true }]} extra="如 deepseek-chat">
          <Input placeholder="deepseek-chat" />
        </Form.Item>
        <Form.Item
          name="api_key"
          label={hasKey ? 'API Key（已配置，留空=保留旧值）' : 'API Key'}
          rules={[{ required: !hasKey }]}
        >
          <Input.Password placeholder={hasKey ? '••••••（已保存）' : 'sk-...'} autoComplete="new-password" />
        </Form.Item>
        <Form.Item name="temperature" label="Temperature">
          <InputNumber min={0} max={2} step={0.1} style={{ width: '100%' }} />
        </Form.Item>
        <Form.Item name="enabled" label="启用 LLM" valuePropName="checked" extra="关闭时 AI 助理降级为内置规则问答">
          <Switch />
        </Form.Item>
        <Button type="primary" loading={saving} onClick={onSave}>保存</Button>
      </Form>
    </div>
  )
}
