/** AI 智能助理 API（v5：多模型选择/参数档位 + table 渲染载荷,O-CLC 集群域语境） */
import { http } from './request'

/** 只读 SQL / 台账查询结果表(渲染协议 text/table;列序由后端固定) */
export interface AssistantTable {
  columns: string[]
  rows: (string | number | boolean | null)[][]
}

export interface AssistantReply {
  answer: string
  hints: string[]
  action?: string
  table?: AssistantTable
}

export interface ModelOption {
  account_id: number
  account_name: string
  provider: string
  models: string[]
  default_model: string
}

export const assistantApi = {
  chat: (question: string, accountId?: number, model?: string, temperature?: number) =>
    http.post<AssistantReply>('/api/assistant/chat', {
      question,
      account_id: accountId || 0,
      model: model || '',
      temperature: temperature ?? 0,
    }),
  models: () => http.get<{ models: ModelOption[] }>('/api/assistant/models'),
  /** 只读 SQL 工具(仅 SELECT/WITH + clc 白名单 + 强制 LIMIT + 审计) */
  sql: (sqlText: string) => http.post<AssistantTable>('/api/assistant/sql', { sql: sqlText }),
}
