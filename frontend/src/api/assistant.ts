/** AI 智能助理 API（v3） */
import { http } from './request'

export interface AssistantReply {
  answer: string
  hints: string[]
  action?: string
}

export const assistantApi = {
  chat: (question: string) => http.post<AssistantReply>('/api/assistant/chat', { question }),
}
