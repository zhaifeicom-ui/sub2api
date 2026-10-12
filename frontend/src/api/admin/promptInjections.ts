import { apiClient } from '../client'

export type PromptInjectionScope = 'group' | 'account'
export type PromptInjectionRole = 'system' | 'developer' | 'user'
export type PromptInjectionPosition = 'prepend' | 'before_first_user' | 'after_last_user'

export interface PromptInjectionRule {
  action?: 'append' | 'replace_system' | 'replace_input' | 'replace_output'
  match?: string
  whole_input?: boolean
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
  version?: number
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
export interface ManagedPolicyEvent { created_at: string; request_id: string; group_id: number; version: number; status: string; transport_status?: string; protocol: string; rule_ids: string[]; input_bytes_before: number; input_bytes_after: number }
export async function getManagedPolicyEvents(): Promise<ManagedPolicyEvent[]> { const { data } = await apiClient.get<ManagedPolicyEvent[]>('/admin/prompt-injections/events'); return data }
