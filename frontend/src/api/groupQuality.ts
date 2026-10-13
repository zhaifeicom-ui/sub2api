import { apiClient } from './client'

export const QUALITY_MODELS = ['gpt-6-astra', 'gpt-6.1-sol'] as const
export type QualityModel = typeof QUALITY_MODELS[number]
export interface QualityConfig {
  monitor_id: number
  monitor_name: string
  provider: string
  enabled: boolean
  models: QualityModel[]
  customer_access: 'hidden' | 'view' | 'run'
  cooldown_seconds: number
  auto_enabled?: boolean
  auto_interval_seconds?: number
  version: number
  available_models: string[]
}
export interface QualityRun {
  id: number
  monitor_id: number
  monitor_name: string
  model: QualityModel
  prompt_version?: string
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
const root = (admin: boolean) => admin ? '/admin/quality-tests/channels' : '/quality-tests/channels'
export async function qualityConfigs(admin: boolean) {
  const { data } = await apiClient.get<{ items: QualityConfig[]; prompt_version: string }>(`${root(admin)}/configs`)
  return data.items
}
export async function saveQualityConfig(config: QualityConfig) {
  const { data } = config.version > 0
    ? await apiClient.put<QualityConfig>(`${root(true)}/${config.monitor_id}/config`, config)
    : await apiClient.post<QualityConfig>(`${root(true)}/configs`, config)
  return data
}
export async function deleteQualityConfig(monitorID: number) {
  await apiClient.delete(`${root(true)}/${monitorID}/config`)
}
export async function qualityHistory(admin: boolean, monitorID: number, model: QualityModel, signal?: AbortSignal) {
  const { data } = await apiClient.get<{ items: QualityRun[] }>(`${root(admin)}/${monitorID}/runs`, { params: { model }, signal })
  return data.items
}
export async function qualityDetail(admin: boolean, id: number, signal?: AbortSignal) {
  const { data } = await apiClient.get<QualityRun>(`${admin ? '/admin/quality-tests' : '/quality-tests'}/channel-runs/${id}`, { signal })
  return data
}
export async function startQualityTest(admin: boolean, monitorID: number, model: QualityModel) {
  const { data } = await apiClient.post<QualityRun>(`${root(admin)}/${monitorID}/runs`, { model })
  return data
}
