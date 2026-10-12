import { apiClient } from './client'

export const QUALITY_MODELS = ['gpt-6-astra', 'gpt-6-sol'] as const
export type QualityModel = typeof QUALITY_MODELS[number]
export interface QualityConfig {
  group_id: number
  group_name: string
  platform: string
  enabled: boolean
  models: QualityModel[]
  customer_access: 'hidden' | 'view' | 'run'
  api_mode: 'responses' | 'chat_completions'
  account_id?: number | null
  cooldown_seconds: number
  auto_enabled?: boolean
  auto_interval_seconds?: number
  version: number
  available_models: string[]
}
export interface QualityRun {
  id: number
  group_id: number
  model: QualityModel
  upstream_model?: string
  prompt_version?: string
  account_id?: number
  api_mode: string
  status: 'running' | 'completed' | 'failed'
  automatic?: boolean
  started_at: string
  deadline_at: string
  finished_at?: string
  duration_ms: number
  response_length: number
  input_tokens: number
  output_tokens: number
  source?: string
  error_code?: string
  error_message?: string
}
const root = (admin: boolean) => admin ? '/admin/quality-tests' : '/quality-tests'
export async function qualityConfigs(admin: boolean) {
  const { data } = await apiClient.get<{ items: QualityConfig[]; prompt_version: string }>(`${root(admin)}/configs`)
  return data.items
}
export async function saveQualityConfig(config: QualityConfig) {
  const { data } = await apiClient.put<QualityConfig>(`${root(true)}/groups/${config.group_id}/config`, config)
  return data
}
export async function qualityHistory(admin: boolean, group: number, model: QualityModel, signal?: AbortSignal) {
  const { data } = await apiClient.get<{ items: QualityRun[] }>(`${root(admin)}/groups/${group}/runs`, { params: { model }, signal })
  return data.items
}
export async function qualityDetail(admin: boolean, id: number, signal?: AbortSignal) {
  const { data } = await apiClient.get<QualityRun>(`${root(admin)}/runs/${id}`, { signal })
  return data
}
export async function startQualityTest(admin: boolean, group: number, model: QualityModel) {
  const { data } = await apiClient.post<QualityRun>(`${root(admin)}/groups/${group}/runs`, { model })
  return data
}
