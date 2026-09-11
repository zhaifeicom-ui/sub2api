import { apiClient } from '../client'

export type PromptInjectionScope = 'group' | 'account'
export type PromptInjectionRole = 'system' | 'developer' | 'user'
export type PromptInjectionPosition = 'prepend' | 'before_first_user' | 'after_last_user'

export interface PromptInjectionRule {
  id: string
  name: string
  enabled: boolean
  scope: PromptInjectionScope
  target_id: number
  group_ids: number[]
  role: PromptInjectionRole
  position: PromptInjectionPosition
  content: string
  models: string[]
  priority: number
}

export interface PromptInjectionConfig {
  rules: PromptInjectionRule[]
}

export async function getPromptInjections(): Promise<PromptInjectionConfig> {
  const { data } = await apiClient.get<PromptInjectionConfig>('/admin/prompt-injections')
  return data
}

export async function updatePromptInjections(config: PromptInjectionConfig): Promise<PromptInjectionConfig> {
  const { data } = await apiClient.put<PromptInjectionConfig>('/admin/prompt-injections', config)
  return data
}
