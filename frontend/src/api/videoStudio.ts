import { apiClient, buildApiUrl } from './client'

export interface VideoTariff {
 resolution: string; min_seconds: number; max_seconds: number; audio: 'any' | 'on' | 'off'; reference: 'any' | 'with' | 'without'
 mode: 'second' | 'job'; rate: number; cost_cny?: number; minimum_seconds?: number; step_seconds?: number
 base_fee?: number; base_cost_cny?: number; image_fee?: number; image_cost_cny?: number; video_fee?: number; video_cost_cny?: number; audio_fee?: number; audio_cost_cny?: number
}
export interface VideoModel {
  disabled?: boolean; spec_confirmed?: boolean; notes?: string; max_references?: number; max_prompt_chars?: number; adaptive?: boolean
  rules?: VideoTariff[]
  id: string; label: string; mode: 'second' | 'job'; rates: Record<string, number>
  cost_cny?: Record<string, number>; min_seconds: number; max_seconds: number
  max_images: number; max_videos: number; max_audios: number; audio: boolean
}
export interface VideoChannel {
  group_id: number; account_id: number; enabled: boolean; public_base_url: string
  max_concurrent: number; models: VideoModel[]
}
export interface VideoOption { key_id: number; key_name: string; group_id: number; group_name: string; models: VideoModel[] }
export interface VideoInput {
  facs?: boolean
  prompt: string; duration: number; resolution: string; aspect_ratio: string
  images: string[]; videos: string[]; audios: string[]; generate_audio: boolean
}
export interface VideoRequest { expected_amount?: number; key_id: number; model: string; input: VideoInput; project_id?: string; shot_id?: string }
export interface VideoTask {
  id: string; key_id: number; project_id: string; shot_id: string; request: VideoRequest
  status: string; outputs: string[] | null; error: string; hold: number; cost: number; settled: boolean
  created_at: string; pricing: { amount: number; model: VideoModel }
}
export interface VideoShot {
  id: string; title: string; prompt: string; duration: number; camera: string; dialogue: string
  sound: string; first_frame: string; images: string[]; videos?: string[]; audios?: string[]; task_id: string
}
export interface VideoProject {
  id: string; revision: number; updated_at?: string
  document: { output?: { key_id: number; model: string; resolution: string; aspect_ratio: string; generate_audio: boolean; facs?: boolean }; title: string; outline: string; character_images: string[]; shots: VideoShot[] }
}
export interface VideoAsset { id: string; name: string; mime: string; bytes: number }
const root = '/video-studio'
export const videoAPI = {
  options: async () => (await apiClient.get<VideoOption[]>(`${root}/options`)).data,
  tasks: async (before = '', project = '') => (await apiClient.get<VideoTask[]>(`${root}/tasks`, { params: { before, project } })).data,
  cancel: async (id: string) => apiClient.post(`${root}/tasks/${id}/cancel`),
  task: async (id: string) => (await apiClient.get<VideoTask>(`${root}/tasks/${id}`)).data,
  quote: async (r: VideoRequest) => (await apiClient.post<{ amount: number }>(`${root}/quote`, r)).data,
  submit: async (r: VideoRequest, idempotency: string) => (await apiClient.post<VideoTask>(`${root}/tasks`, r, { headers: { 'Idempotency-Key': idempotency } })).data,
  submitBatch: async (requests: VideoRequest[], idempotency: string) => (await apiClient.post<VideoTask[]>(`${root}/tasks/batch`, { requests }, { headers: { 'Idempotency-Key': idempotency }, timeout: 120000 })).data,
  projects: async () => (await apiClient.get<VideoProject[]>(`${root}/projects`)).data,
  archive: async (id: string) => apiClient.delete(`${root}/projects/${id}`),
  save: async (p: VideoProject) => (await apiClient.put<VideoProject>(`${root}/projects`, p)).data,
  plan: async (key_id: number, model: string, outline: string, count: number) => (await apiClient.post<VideoShot[]>(`${root}/plan`, { key_id, model, outline, count }, { timeout: 160000 })).data,
  assets: async () => (await apiClient.get<VideoAsset[]>(`${root}/assets`)).data,
  upload: async (file: File) => { const form = new FormData(); form.append('file', file); return (await apiClient.post<VideoAsset>(`${root}/assets`, form, { headers: { 'Content-Type': 'multipart/form-data' }, timeout: 90000 })).data },
  deleteAsset: async (id: string) => apiClient.delete(`${root}/assets/${id}`),
  assetURL: (id: string) => buildApiUrl(`/video-assets/${encodeURIComponent(id)}`),
  channelChoices: async () => (await apiClient.get<{ groups: { id: number; name: string }[]; accounts: { id: number; name: string }[] }>(`${root}/channel-choices`)).data,
  importCatalog: async (account_id: number) => (await apiClient.post<VideoModel[]>(`${root}/catalog/import`, { account_id }, { timeout: 65000 })).data,
  settings: async () => (await apiClient.get<VideoChannel[]>(`${root}/settings`)).data,
  saveSettings: async (c: VideoChannel) => apiClient.put(`${root}/settings`, c),
  reviews: async () => (await apiClient.get<VideoTask[]>(`${root}/reviews`)).data,
  reconcile: async (id: string, r: { upstream_id?: string; refund?: boolean; reason?: string }) => apiClient.post(`${root}/reviews/${id}`, r)
}

export const videoStatus: Record<string, string> = { pending: '等待提交', submitting: '提交中', queued: '排队中', in_progress: '生成中', completed: '已完成', failed: '失败 · 已退款', needs_review: '待核对 · 金额冻结' }
export function estimateVideo(model: VideoModel | undefined, resolution: string, seconds: number, input?: VideoInput): number {
  if (!model || !Number.isFinite(seconds) || seconds < model.min_seconds || seconds > model.max_seconds) return 0
  if (model.rules?.length) {
    if (!input) return 0
    const hasRef = input.images.length + input.videos.length + input.audios.length > 0
    const rules = model.rules.filter(r => r.resolution === resolution && seconds >= r.min_seconds && seconds <= r.max_seconds && (r.audio === 'any' || (r.audio === 'on') === input.generate_audio) && (r.reference === 'any' || (r.reference === 'with') === hasRef))
    if (rules.length !== 1) return 0
    const r = rules[0]!; let units = 1
    if (r.mode === 'second') { units = Math.max(seconds, r.minimum_seconds || 0); if (r.step_seconds) units = Math.ceil(units / r.step_seconds) * r.step_seconds }
    return Math.round(((r.base_fee || 0) + r.rate * units + (r.image_fee || 0) * input.images.length + (r.video_fee || 0) * input.videos.length + (r.audio_fee || 0) * input.audios.length) * 1e8) / 1e8
  }
  const price = model.rates[resolution]
  return price === undefined ? 0 : Math.round(price * (model.mode === 'second' ? seconds : 1) * 1e8) / 1e8
}
export function emptyShot(): VideoShot { return { id: crypto.randomUUID(), title: '新镜头', prompt: '', duration: 5, camera: '', dialogue: '', sound: '', first_frame: '', images: [], videos: [], audios: [], task_id: '' } }
export function initialChannel(): VideoChannel {
  return { group_id: 0, account_id: 0, enabled: false, public_base_url: window.location.origin, max_concurrent: 1, models: [] }
}

// Cost is CNY, whereas rates debit station credits. Conversion must be explicit.
export function markUpVideoModel(model: VideoModel, cnyPerCredit: number): VideoModel {
  if (!Number.isFinite(cnyPerCredit) || cnyPerCredit <= 0) throw new Error('请填写每站内额度对应的人民币金额')
  const result: VideoModel = JSON.parse(JSON.stringify(model))
  const price = (cost: number | undefined, optional = false) => {
    if (optional && (cost === undefined || cost === 0)) return 0
    if (!Number.isFinite(cost) || !cost || cost < 0) throw new Error('请先填写已确认的实际成本')
    const sale = Math.round((cost * 1.2 / cnyPerCredit + Number.EPSILON) * 100) / 100
    if (sale <= 0 || sale <= cost / cnyPerCredit) throw new Error('两位小数精度不足以保留利润，请调整计价单位或手动核对售价')
    return sale
  }
  for (const res of Object.keys(result.rates)) result.rates[res] = price(result.cost_cny?.[res])
  for (const rule of result.rules || []) {
    rule.rate = price(rule.cost_cny)
    rule.base_fee = price(rule.base_cost_cny, true)
    rule.image_fee = price(rule.image_cost_cny, true)
    rule.video_fee = price(rule.video_cost_cny, true)
    rule.audio_fee = price(rule.audio_cost_cny, true)
  }
  return result
}
