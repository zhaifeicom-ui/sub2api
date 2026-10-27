import type { VideoInput, VideoModel, VideoProject, VideoShot } from './videoStudio'

export function shotInput(settings: VideoInput, shot: VideoShot, characters: string[]): VideoInput {
  return {
    ...settings,
    prompt: [shot.prompt, shot.camera && `运镜：${shot.camera}`, shot.dialogue && `对白：${shot.dialogue}`, shot.sound && `声音：${shot.sound}`].filter(Boolean).join('\n'),
    duration: shot.duration,
    images: [...new Set([shot.first_frame, ...characters, ...(shot.images || [])].filter(Boolean))],
    videos: [...(shot.videos || [])],
    audios: [...(shot.audios || [])]
  }
}

// Also used while picking assets, before a prompt or duration has been entered.
export function videoReferenceError(model: VideoModel | undefined, input: Pick<VideoInput, 'images' | 'videos' | 'audios'>): string {
  if (!model) return '请选择视频密钥和模型'
  if (model.max_references && input.images.length + input.videos.length + input.audios.length > model.max_references) return `参考素材总数最多 ${model.max_references} 个（图片、视频、音频合计）`
  if (input.images.length > model.max_images) return `参考图合计 ${input.images.length} 张，当前模型最多 ${model.max_images} 张（包含角色图和首帧）`
  if (input.videos.length > model.max_videos) return `参考视频最多 ${model.max_videos} 个，请移除多余素材`
  if (input.audios.length > model.max_audios) return `参考音频最多 ${model.max_audios} 个，请移除多余素材`
  return ''
}

export function videoInputError(model: VideoModel | undefined, input: VideoInput): string {
  if (!model) return '请选择视频密钥和模型'
  if (model.disabled) return '此模型尚未开放'
  const referenceError = videoReferenceError(model, input)
  if (referenceError) return referenceError
  if (!input.prompt.trim()) return '请填写画面描述'
  if ([...input.prompt.trim()].length > (model.max_prompt_chars || 7000)) return `合并后的提示词不能超过 ${model.max_prompt_chars || 7000} 字，请缩短画面描述、对白或运镜说明`
  if (!Number.isInteger(input.duration) || input.duration < model.min_seconds || input.duration > model.max_seconds) return `当前模型要求时长为 ${model.min_seconds}～${model.max_seconds} 秒的整数`
  if (!(input.resolution in model.rates)) return '当前模型不支持所选清晰度'
  if (input.aspect_ratio === 'adaptive' && (!model.adaptive || (model.id === 'MiniMax-H3' && input.images.length + input.videos.length === 0))) return '自适应画幅需要支持的模型；H3 还需图片或视频参考'
  if (input.generate_audio && !model.audio) return '当前模型不支持生成声音'
  return ''
}

// Validate the whole import before replacing the open project. Missing media arrays
// are accepted for projects exported before per-shot video/audio support.
export function parseVideoProject(raw: string): VideoProject {
  const value = JSON.parse(raw)
  const d = value?.document
  if (!d || typeof d.title !== 'string' || typeof d.outline !== 'string' || !Array.isArray(d.shots) || !Array.isArray(d.character_images)) throw new Error('剧本文件格式不正确')
  const strings = (v: unknown, max: number): string[] => {
    if (!Array.isArray(v) || v.length > max || !v.every(x => typeof x === 'string')) throw new Error('剧本素材列表格式不正确或超过上限')
    return [...v]
  }
  if ([...d.title].length > 120 || [...d.outline].length > 20000 || d.shots.length > 40) throw new Error('剧本超出限制：标题 120 字、大纲 2 万字、40 个镜头')
  const shots: VideoShot[] = d.shots.map((s: Record<string, unknown>) => {
    if (!s || !Number.isInteger(s.duration) || Number(s.duration) < 1 || Number(s.duration) > 600) throw new Error('镜头时长必须是 1～600 秒的整数')
    for (const k of ['title', 'prompt', 'camera', 'dialogue', 'sound', 'first_frame']) if (typeof s[k] !== 'string') throw new Error('镜头字段格式不正确')
    if ([...String(s.prompt)].length > 16000) throw new Error('镜头提示词不能超过 16000 字')
    return { id: crypto.randomUUID(), title: String(s.title), prompt: String(s.prompt), duration: Number(s.duration), camera: String(s.camera), dialogue: String(s.dialogue), sound: String(s.sound), first_frame: String(s.first_frame), images: strings(s.images ?? [], 30), videos: strings(s.videos ?? [], 10), audios: strings(s.audios ?? [], 10), task_id: '' }
  })
  const output = d.output
  if (output && (!Number.isSafeInteger(output.key_id) || typeof output.model !== 'string' || typeof output.resolution !== 'string' || !['16:9', '9:16', '1:1', '4:3', '3:4', '21:9', 'adaptive'].includes(output.aspect_ratio) || typeof output.generate_audio !== 'boolean' || (output.facs !== undefined && typeof output.facs !== 'boolean'))) throw new Error('剧本输出设置格式不正确')
  return { id: '', revision: 0, document: { title: d.title, outline: d.outline, character_images: strings(d.character_images, 5), shots, ...(output ? { output: { ...output } } : {}) } }
}
