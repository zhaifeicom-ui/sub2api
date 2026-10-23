<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import VideoApiDocs from '@/components/video/VideoApiDocs.vue'
import VideoStudioSettings from '@/components/video/VideoStudioSettings.vue'
import { parseVideoProject, shotInput, videoInputError, videoReferenceError } from '@/api/videoStudioInputs'
import { useAuthStore } from '@/stores/auth'
import { list as listKeys } from '@/api/keys'
import type { ApiKey } from '@/types'
import { emptyShot, estimateVideo, videoAPI, videoStatus, type VideoAsset, type VideoInput, type VideoOption, type VideoProject, type VideoRequest, type VideoShot, type VideoTask } from '@/api/videoStudio'

const auth = useAuthStore()
const mode = ref<'single' | 'story' | 'settings' | 'docs'>('single')
const options = ref<VideoOption[]>([])
const keyID = ref(0)
const modelID = ref('')
const input = ref<VideoInput>({ prompt: '', duration: 5, resolution: '768p', aspect_ratio: '16:9', images: [], videos: [], audios: [], generate_audio: false, facs: true })
const tasks = ref<VideoTask[]>([])
const olderTasks = ref<VideoTask[]>([])
const filter = ref('all')
const projects = ref<VideoProject[]>([])
const project = ref<VideoProject>({ id: '', revision: 0, document: { title: '未命名剧本', outline: '', character_images: [], shots: [] } })
const selectedShot = ref('')
const assets = ref<VideoAsset[]>([])
const keys = ref<ApiKey[]>([])
const libraryAssets = computed(() => libraryTarget.value === 'reference' ? assets.value : assets.value.filter(a => a.mime.startsWith('image/')))
const plannerKeyID = ref(0)
const plannerModel = ref('')
const shotCount = ref(6)
const loading = ref(true)
const busy = ref(false)
const planning = ref(false)
const uploading = ref(false)
const batchBusy = ref(false)
const message = ref('')
const saveState = ref('已保存')
const preview = ref<VideoTask | null>(null)
const showLibrary = ref(false)
const libraryTarget = ref<'characters' | 'first' | 'reference'>('reference')
const zoom = ref(1)
const historyOpen = ref(true)
const quote = ref<number | null>(null)
const confirming = ref<{ requests: VideoRequest[]; total: number } | null>(null)
const intents = new Map<string, string>()
let poll: ReturnType<typeof setInterval> | undefined
let autosave: ReturnType<typeof setTimeout> | undefined
let saving: Promise<void> | null = null
let ignoreChanges = false
let revisionCounter = 0
let stopped = false
let polling = false
const draftKey = computed(() => `sub2api-video-draft:${auth.user?.id}`)
const chosen = computed(() => options.value.find(o => o.key_id === keyID.value))
const models = computed(() => (chosen.value?.models || []).filter(m => !m.disabled))
const model = computed(() => models.value.find(m => m.id === modelID.value))
const resolutions = computed(() => Object.keys(model.value?.rates || {}))
const shot = computed(() => project.value.document.shots.find(s => s.id === selectedShot.value))
const allTasks = computed(() => [...new Map([...olderTasks.value, ...tasks.value].map(t => [t.id, t])).values()].sort((a, b) => b.created_at.localeCompare(a.created_at) || b.id.localeCompare(a.id)))
const taskMap = computed(() => new Map(allTasks.value.map(t => [t.id, t])))
const visibleTasks = computed(() => allTasks.value.filter(t => filter.value === 'all' || (filter.value === 'running' ? !t.settled : t.status === filter.value)))
const completedShots = computed(() => project.value.document.shots.map(s => ({ shot: s, task: taskForShot(s) })).filter(x => x.task?.status === 'completed'))
const estimatedSingle = computed(() => estimateVideo(model.value, input.value.resolution, input.value.duration, input.value))
const singleError = computed(() => videoInputError(model.value, input.value))
const shotError = (s: VideoShot) => videoInputError(model.value, shotInput(input.value, s, project.value.document.character_images))
const canSubmit = computed(() => !!model.value && !!input.value.prompt.trim() && !busy.value && !batchBusy.value)
const errText = (e: unknown) => String((e as { message?: string })?.message || '操作失败，请稍后重试')
const money = (n: number) => `$${Number(n || 0).toFixed(2)}`
const state = (t?: VideoTask) => t ? videoStatus[t.status] || t.status : '未生成'
const assetURL = videoAPI.assetURL
const safeURL = (s: string) => { try { return new URL(s).protocol === 'https:' ? s : '' } catch { return '' } }
function taskForShot(s: VideoShot): VideoTask | undefined { return (s.task_id && taskMap.value.get(s.task_id)) || allTasks.value.find(t => t.project_id === project.value.id && t.shot_id === s.id) || undefined }
function normalizeModel() {
  if (!models.value.some(m => m.id === modelID.value)) modelID.value = models.value[0]?.id || ''
  const m = model.value
  if (!m) return
  if (!resolutions.value.includes(input.value.resolution)) input.value.resolution = resolutions.value[0] || ''
  input.value.duration = Math.max(m.min_seconds, Math.min(m.max_seconds, input.value.duration))
  if (!m.audio) input.value.generate_audio = false
  if (!m.adaptive && input.value.aspect_ratio === 'adaptive') input.value.aspect_ratio = '16:9'
}
watch([keyID, modelID, options], normalizeModel)
function outputSettings() { return { key_id: keyID.value, model: modelID.value, resolution: input.value.resolution, aspect_ratio: input.value.aspect_ratio, generate_audio: input.value.generate_audio, facs: input.value.facs } }
function restoreOutput() {
  const output = project.value.document.output
  if (!output) return
  if (options.value.some(o => o.key_id === output.key_id)) keyID.value = output.key_id
  modelID.value = output.model
  input.value.resolution = output.resolution; input.value.aspect_ratio = output.aspect_ratio; input.value.generate_audio = output.generate_audio; input.value.facs = output.facs ?? true
  normalizeModel()
}
watch([keyID, modelID, () => input.value.resolution, () => input.value.aspect_ratio, () => input.value.generate_audio, () => input.value.facs], () => {
  if (!ignoreChanges && mode.value === 'story') project.value.document.output = outputSettings()
})
watch(project, () => {
  if (ignoreChanges || mode.value !== 'story') return
  revisionCounter++
  saveState.value = '未保存'
  localStorage.setItem(draftKey.value, JSON.stringify(project.value))
  if (autosave) clearTimeout(autosave)
  autosave = setTimeout(() => { void saveProject().catch(() => {}) }, 1000)
}, { deep: true })
watch([keyID, modelID, input], () => { quote.value = null }, { deep: true })
async function saveProject(): Promise<void> {
  if (saving) { await saving; if (saveState.value === '未保存') return saveProject(); return }
  if (!project.value.document.title.trim()) project.value.document.title = '未命名剧本'
  const tick = revisionCounter
  const payload = JSON.parse(JSON.stringify(project.value)) as VideoProject
  payload.document.output = outputSettings()
  saveState.value = '保存中…'
  saving = (async () => {
    try {
      const saved = await videoAPI.save(payload)
      ignoreChanges = true
      project.value.id = saved.id; project.value.revision = saved.revision
      await Promise.resolve(); ignoreChanges = false
      projects.value = [saved, ...projects.value.filter(p => p.id !== saved.id)]
      saveState.value = revisionCounter === tick ? '已保存' : '未保存'
      if (saveState.value === '已保存') localStorage.removeItem(draftKey.value)
    } catch (e) { saveState.value = '保存失败 · 本地草稿已保留'; message.value = `${errText(e)}。请导出剧本备份，避免覆盖其他窗口的修改。`; throw e }
  })()
  try { await saving } finally { saving = null }
}
async function switchProject(id: string) {
  try { if (saveState.value !== '已保存') await saveProject() } catch { return }
  const p = projects.value.find(p => p.id === id)
  if (!p) return
  ignoreChanges = true; project.value = JSON.parse(JSON.stringify(p)); restoreOutput(); selectedShot.value = project.value.document.shots[0]?.id || ''; saveState.value = '已保存'
  await Promise.resolve(); ignoreChanges = false
  try { olderTasks.value.push(...await videoAPI.tasks('',id)) } catch { /* Keep saved project editable if history refresh fails. */ }
  for (const s of project.value.document.shots) { if (s.task_id && !taskMap.value.has(s.task_id)) { try { olderTasks.value.push(await videoAPI.task(s.task_id)) } catch { /* History still permits independent lookup by the owner. */ } } }
}
async function newProject() {
  try { if (saveState.value !== '已保存') await saveProject() } catch { return }
  ignoreChanges = true; project.value = { id: '', revision: 0, document: { title: '新剧本', outline: '', character_images: [], shots: [] } }; selectedShot.value = ''; await Promise.resolve(); ignoreChanges = false
  await saveProject().catch(() => {})
}
async function archiveProject() {
 if (!project.value.id || !window.confirm('归档这个剧本？已提交的视频任务继续处理，历史记录仍保留。')) return
 try { await videoAPI.archive(project.value.id); projects.value=projects.value.filter(p=>p.id!==project.value.id);localStorage.removeItem(draftKey.value);saveState.value='已保存';if(projects.value[0])await switchProject(projects.value[0].id);else await newProject() } catch(e){message.value=errText(e)}
}
function addShot() { if (project.value.document.shots.length >= 40) { message.value = '每个剧本最多 40 个镜头'; return } const s = emptyShot(); project.value.document.shots.push(s); selectedShot.value = s.id }
function removeShot(s: VideoShot) { if (taskForShot(s) && !taskForShot(s)?.settled) { message.value = '镜头任务仍在处理中，请等待完成'; return } project.value.document.shots = project.value.document.shots.filter(x => x.id !== s.id); selectedShot.value = project.value.document.shots[0]?.id || '' }
function moveShot(index: number, delta: number) { const target = index + delta; if (target < 0 || target >= project.value.document.shots.length) return; const arr = project.value.document.shots; [arr[index], arr[target]] = [arr[target]!, arr[index]!] }
async function plan() {
  const targetProject = project.value
  if (!plannerKeyID.value || !plannerModel.value.trim()) { message.value = '请选择聊天 Key 并填写可用模型 ID'; return }
  if (project.value.document.shots.length && !window.confirm('重新拆分会替换当前镜头，历史视频仍保留。继续？')) return
  planning.value = true; message.value = ''
  try { const shots = await videoAPI.plan(plannerKeyID.value, plannerModel.value, project.value.document.outline, shotCount.value); if (project.value !== targetProject) { message.value = '拆分期间切换了剧本，返回的镜头未覆盖当前项目'; return } project.value.document.shots = shots; selectedShot.value = shots[0]?.id || ''; await saveProject(); message.value = `已拆分 ${shots.length} 个镜头，可逐镜编辑后生成` } catch (e) { message.value = errText(e) } finally { planning.value = false }
}
function requestFor(s?: VideoShot): VideoRequest {
  const data = s ? shotInput(input.value, s, project.value.document.character_images) : JSON.parse(JSON.stringify(input.value)) as VideoInput
  return { key_id: keyID.value, model: modelID.value, input: data, ...(s ? { project_id: project.value.id, shot_id: s.id } : {}) }
}
async function prepareSubmit(shots?: VideoShot[]) {
  if (!model.value) { message.value = '请选择视频 Key 和模型'; return }
  busy.value = true; message.value = ''
  try {
    if (shots) await saveProject()
    const requests = shots ? shots.map(s => requestFor(s)) : [requestFor()]
    if (!requests.length) { message.value = '没有需要生成的镜头'; return }
    let total = 0
    for (let i = 0; i < requests.length; i++) { const error = videoInputError(model.value, requests[i]!.input); if (error) throw new Error(`${shots ? `镜头 ${i + 1}：` : ''}${error}`) }
    for (const req of requests) { const result = await videoAPI.quote(req); req.expected_amount = result.amount; total += result.amount }
    confirming.value = { requests, total }; quote.value = total
  } catch (e) { message.value = errText(e) } finally { busy.value = false }
}
async function confirmSubmit() {
  const confirmed = confirming.value
  if (!confirmed) return
  confirming.value = null; batchBusy.value = true
  const payload = JSON.stringify(confirmed.requests)
  const storageKey = `${draftKey.value}:intent:batch:${project.value.id || 'single'}`
  let idem = intents.get(payload)
  if (!idem) {
    try { const prev = JSON.parse(localStorage.getItem(storageKey) || 'null') as { payload: string; idem: string } | null; if (prev?.payload === payload) idem = prev.idem } catch { /* invalid local draft */ }
    idem ||= crypto.randomUUID(); intents.set(payload, idem); localStorage.setItem(storageKey, JSON.stringify({ payload, idem }))
  }
  try {
    const accepted = await videoAPI.submitBatch(confirmed.requests, idem)
    tasks.value = [...accepted, ...tasks.value.filter(x => !accepted.some(t => t.id === x.id))]
    intents.delete(payload); localStorage.removeItem(storageKey)
    for (const t of accepted) { if (t.shot_id) { const s = project.value.document.shots.find(x => x.id === t.shot_id); if (s) s.task_id = t.id } }
    if (accepted.some(t => t.shot_id)) await saveProject()
    message.value = `已接受 ${accepted.length} 个任务并保存至服务器，关闭网页后整批仍继续排队生成。`
    await auth.refreshUser()
  } catch (e) { message.value = `${errText(e)}。请先刷新历史确认是否已接受；重试同一内容将沿用提交编号，避免重复冻结。` } finally { batchBusy.value = false; await refreshTasks() }
}

function openLibrary(target: typeof libraryTarget.value) { libraryTarget.value = target; showLibrary.value = true }
function chooseAsset(a: VideoAsset) {
  const target = libraryTarget.value
  const check = (candidate: Pick<VideoInput, 'images' | 'videos' | 'audios'>) => {
    const error = videoReferenceError(model.value, candidate)
    if (error) message.value = error
    return !error
  }
  if (target === 'characters') {
    if (!a.mime.startsWith('image/')) return
    if (project.value.document.character_images.includes(a.id)) { showLibrary.value = false; return }
    if (project.value.document.character_images.length >= 5) { message.value = '角色参考最多5张，计入每个镜头的参考图总数'; return }
    const characters = [...project.value.document.character_images, a.id]
    const shots = project.value.document.shots
    if (shots.some(s => !check(shotInput(input.value, s, characters)))) return
    if (!shots.length && !check({ images: characters, videos: [], audios: [] })) return
    project.value.document.character_images = characters
  } else if (target === 'first') {
    if (!a.mime.startsWith('image/') || !shot.value) return
    if (!check(shotInput(input.value, { ...shot.value, first_frame: a.id }, project.value.document.character_images))) return
    shot.value.first_frame = a.id
  } else {
    const targetInput = mode.value === 'story' && shot.value ? shot.value : input.value
    const field = a.mime.startsWith('image/') ? 'images' : a.mime.startsWith('video/') ? 'videos' : 'audios'
    const list = targetInput[field] || []
    if (list.includes(a.id)) { showLibrary.value = false; return }
    const candidate = { ...targetInput, [field]: [...list, a.id] }
    const actual = mode.value === 'story' && shot.value
      ? shotInput(input.value, candidate as VideoShot, project.value.document.character_images)
      : candidate as VideoInput
    if (!check(actual)) return
    targetInput[field] = [...list, a.id]
  }
  showLibrary.value = false
}

async function upload(event: Event) {
  const el = event.target as HTMLInputElement; const file = el.files?.[0]; if (!file) return
  if (file.size > 30 * 1024 * 1024) { message.value = '单个素材不能超过 30MB'; return }
  uploading.value = true
  try { const a = await videoAPI.upload(file); assets.value.unshift(a); chooseAsset(a) } catch (e) { message.value = errText(e) } finally { uploading.value = false; el.value = '' }
}
async function deleteAsset(a: VideoAsset) { if (!window.confirm(`删除素材“${a.name}”？`)) return; try { await videoAPI.deleteAsset(a.id); assets.value = assets.value.filter(x => x.id !== a.id) } catch (e) { message.value = errText(e) } }
async function refreshTasks() {
  if (polling || stopped) return
  polling = true
  try { const recent = await videoAPI.tasks(); const changed = recent.some(t => t.settled && tasks.value.some(old => old.id === t.id && !old.settled)); tasks.value = recent; if (changed) await auth.refreshUser(); for (let i = 0; i < olderTasks.value.length; i++) if (!olderTasks.value[i]!.settled) olderTasks.value[i] = await videoAPI.task(olderTasks.value[i]!.id) } catch (e) { message.value = `进度刷新失败：${errText(e)}，后台任务继续运行` } finally { polling = false }
}
async function cancelTask(t: VideoTask) { try { await videoAPI.cancel(t.id); await refreshTasks(); await auth.refreshUser() } catch (e) { message.value=errText(e) } }
async function moreHistory() { const last = allTasks.value.at(-1); if (!last) return; try { const more = await videoAPI.tasks(last.id); olderTasks.value.push(...more); if (!more.length) message.value = '已加载全部历史' } catch (e) { message.value = errText(e) } }
async function loadOptions() { options.value = await videoAPI.options(); if (!options.value.some(o => o.key_id === keyID.value)) keyID.value = options.value[0]?.key_id || 0; normalizeModel() }
function exportProject() {
  const doc = { ...project.value, videos: completedShots.value.map(x => ({ shot: x.shot.title, urls: x.task?.outputs })) }
  const url = URL.createObjectURL(new Blob([JSON.stringify(doc, null, 2)], { type: 'application/json' })); const a = document.createElement('a'); a.href = url; a.download = `${project.value.document.title || '剧本'}.json`; a.click(); URL.revokeObjectURL(url)
}
async function importProject(event: Event) {
  const el = event.target as HTMLInputElement; const f = el.files?.[0]; if (!f) return; if (f.size > 1024 * 1024) { message.value = '剧本文件不能超过 1MB'; el.value = ''; return }
  try { const p = parseVideoProject(await f.text()); if (saveState.value !== '已保存') await saveProject(); if (!window.confirm('导入为一个新剧本？素材需属于当前账号，原剧本会保留。')) return; ignoreChanges = true; project.value = p; restoreOutput(); await Promise.resolve(); ignoreChanges = false; await saveProject(); selectedShot.value = p.document.shots[0]?.id || '' } catch (e) { message.value = errText(e) } finally { el.value = '' }
}
function beforeUnload(e: BeforeUnloadEvent) { if (saveState.value !== '已保存' || batchBusy.value) { e.preventDefault(); e.returnValue = '' } }
onMounted(async () => {
  window.addEventListener('beforeunload', beforeUnload)
  try {
    const results = await Promise.allSettled([loadOptions(), videoAPI.projects(), videoAPI.assets(), listKeys(1, 100)] as const)
    const [, ps, as, ks] = results
    if (ps.status === 'fulfilled') projects.value = ps.value
    if (as.status === 'fulfilled') assets.value = as.value
    if (ks.status === 'fulfilled') { keys.value = ks.value.items.filter(k => k.group?.platform !== 'video'); plannerKeyID.value = keys.value[0]?.id || 0 }
    const failures = results.filter(r => r.status === 'rejected')
    if (failures.length) message.value = `部分数据加载失败，可刷新页面重试：${failures.map(r => errText(r.reason)).join('；')}`
    const draft = localStorage.getItem(draftKey.value)
    let restored = false
    if (draft) {
      try {
        parseVideoProject(draft)
        if (window.confirm('发现未保存的本地剧本草稿，是否恢复？')) { project.value = JSON.parse(draft); restoreOutput(); mode.value = 'story'; saveState.value = '未保存'; restored = true }
      } catch { message.value = '本地草稿格式损坏，已保留原数据；服务器项目仍可正常打开。' }
    }
    if (!restored && projects.value[0]) await switchProject(projects.value[0].id)
    await refreshTasks()
  } catch (e) { message.value = errText(e) } finally { loading.value = false; if (!stopped) poll = setInterval(() => { if (!document.hidden) void refreshTasks() }, 6000) }
})
onBeforeUnmount(() => { stopped = true; if (poll) clearInterval(poll); if (autosave) clearTimeout(autosave); window.removeEventListener('beforeunload', beforeUnload) })
</script>

<template>
  <AppLayout>
    <div class="video-studio">
      <header class="studio-top"><div><span class="eyebrow">CREATIVE STUDIO</span><h1>视频工作台 <span>把灵感变成画面</span></h1></div><div class="top-actions"><span class="wallet">余额 {{ money(auth.user?.balance || 0) }}</span><button @click="refreshTasks">刷新进度</button></div></header>
      <div class="studio-toolbar"><nav aria-label="工作模式"><button :class="{ active: mode === 'single' }" @click="mode = 'single'">单次生成</button><button :class="{ active: mode === 'story' }" @click="mode = 'story'">剧本与分镜</button><button @click="mode = 'docs'" :class="{ active: mode === 'docs' }">API 调用说明</button><button v-if="auth.user?.role === 'admin'" :class="{ active: mode === 'settings' }" @click="mode = 'settings'">渠道与计费</button></nav><button v-if="mode !== 'settings'" @click="historyOpen = !historyOpen">{{ historyOpen ? '收起历史' : '历史记录' }}</button></div>
      <div v-if="message" role="status" class="studio-message">{{ message }}<button aria-label="关闭提示" @click="message = ''">×</button></div>
      <div v-if="loading" class="empty-state">正在加载视频工作台…</div>
      <VideoApiDocs v-else-if="mode === 'docs'" />
      <VideoStudioSettings v-else-if="mode === 'settings' && auth.user?.role === 'admin'" @saved="loadOptions" />
      <template v-else>
        <div v-if="!options.length" class="setup-notice">暂无可用视频分组。请先配置并开启视频渠道，并创建绑定该分组的 API Key。<router-link to="/keys">管理密钥 →</router-link></div>
        <div class="model-bar">
          <label>视频分组 / 密钥<select v-model="keyID"><option v-if="!options.length" :value="0">暂无视频密钥</option><option v-for="o in options" :key="o.key_id" :value="o.key_id">{{ o.group_name }} · {{ o.key_name }}</option></select></label>
          <label>视频模型<select v-model="modelID"><option v-if="!models.length" value="">请选择分组</option><option v-for="m in models" :key="m.id" :value="m.id">{{ m.label || m.id }}</option></select></label>
          <div class="model-price" v-if="model && !model.rules?.length"><span>{{ model.mode === 'job' ? '按条计费' : '按秒计费' }}</span><strong>{{ money(model.rates[input.resolution] || 0) }} / {{ model.mode === 'job' ? '条' : '秒' }}</strong><small>成功结算，明确失败退回冻结额</small></div><div v-else-if="model" class="model-price"><span>{{ model.id === 'MiniMax-H3' ? '按秒计费 · 整单保留两位小数' : '按条件计费' }}</span><strong>{{ estimatedSingle > 0 ? money(estimatedSingle) + ' / 本次' : '请选择有效参数' }}</strong><small>按当前时长和素材预估，最终以提交前报价为准</small></div>
        </div>
        <p v-if="modelID === 'MiniMax-H3'" class="hint">H3 的声音由上游模型决定，当前接口不提供静音开关。</p>
        <label v-if="modelID === 'seedance2.5-yx-stable'"><input v-model="input.facs" type="checkbox" /> FACS 参考图片预处理（不额外收费，仅普通参考图）</label>
        <p v-if="model" class="hint model-limits">模型限制：{{ model.min_seconds }}～{{ model.max_seconds }} 秒 · {{ resolutions.join(' / ').toUpperCase() }} · 参考图 {{ model.max_images }} 张 / 视频 {{ model.max_videos }} 个 / 音频 {{ model.max_audios }} 个<span v-if="model.max_references"> · 三类素材合计最多 {{ model.max_references }} 个</span>。切换模型后请核对现有素材。</p>
        <div class="workspace" :class="{ 'with-history': historyOpen, story: mode === 'story' }">
          <aside v-if="mode === 'story'" class="script-panel">
            <div class="panel-title"><h2>剧本</h2><button @click="newProject">＋ 新建</button></div>
            <select :value="project.id" @change="switchProject(($event.target as HTMLSelectElement).value)"><option value="">未保存剧本</option><option v-for="p in projects" :key="p.id" :value="p.id">{{ p.document.title }}</option></select>
            <input v-model="project.document.title" aria-label="剧本名称" placeholder="为剧本起个名字" maxlength="120" />
            <textarea v-model="project.document.outline" rows="7" placeholder="写下故事大纲，例如：雨夜的便利店，女孩买完饮料离开，发现门外有一只淋湿的小猫……" maxlength="20000" />
            <details class="planner-settings" open><summary>AI 拆分设置</summary><label>聊天 Key<select v-model="plannerKeyID"><option :value="0">请选择</option><option v-for="k in keys" :key="k.id" :value="k.id">{{ k.name }}</option></select></label><label>聊天模型 ID<input v-model="plannerModel" placeholder="填写该 Key 可调用的模型名" /></label><label>镜头数量<input v-model.number="shotCount" type="number" min="1" max="24" /></label><small>拆分剧本调用聊天模型，按该 Key 原有规则收费。</small></details>
            <button class="primary full" :disabled="planning || !project.document.outline.trim()" @click="plan">{{ planning ? '正在构思镜头…' : '✦ 拆成分镜' }}</button>
            <div class="shot-list-title"><h3>镜头 {{ project.document.shots.length }}</h3><button @click="addShot">＋</button></div>
            <button v-for="(s, i) in project.document.shots" :key="s.id" class="shot-list-item" :class="{ selected: selectedShot === s.id }" @click="selectedShot = s.id"><span class="shot-number">{{ String(i + 1).padStart(2, '0') }}</span><span>{{ s.title }}<small>{{ s.duration }} 秒 · {{ state(taskForShot(s)) }}</small></span></button>
            <div class="project-actions"><span>{{ saveState }}</span><button @click="saveProject().catch(() => {})">保存</button><button @click="exportProject">导出</button><button :disabled="!project.id" @click="archiveProject">归档</button><label class="file-button">导入<input type="file" accept="application/json" @change="importProject" /></label></div>
          </aside>
          <main class="creation-panel">
            <div v-if="mode === 'single'" class="single-editor">
              <div class="section-heading"><h2>创作一段视频</h2><span>文字描述与参考素材，自由组合</span></div>
              <label class="prompt-label">画面描述<textarea v-model="input.prompt" class="main-prompt" rows="6" :maxlength="model?.max_prompt_chars || 7000" placeholder="描述主体、动作、场景、光线与镜头运动。越具体，画面越接近你的想象。" /></label>
              <div class="form-row"><label>时长 <strong>{{ input.duration }} 秒</strong><input v-model.number="input.duration" type="range" :min="model?.min_seconds || 4" :max="model?.max_seconds || 15" step="1" /><small>{{ model?.min_seconds || 4 }}–{{ model?.max_seconds || 15 }} 秒</small></label></div>
              <div class="form-row"><label>清晰度<div class="chips"><button v-for="r in resolutions" :key="r" :class="{ selected: input.resolution === r }" @click="input.resolution = r">{{ r.toUpperCase() }}</button></div></label><label v-if="modelID !== 'MiniMax-H3'">声音<div class="chips"><button :class="{ selected: !input.generate_audio }" @click="input.generate_audio = false">无声</button><button :disabled="!model?.audio" :class="{ selected: input.generate_audio }" @click="input.generate_audio = true">有声</button></div></label></div>
              <label>画面比例<div class="ratio-grid"><button v-for="r in ['16:9', '9:16', '1:1', '4:3', '3:4', '21:9', ...(model?.adaptive ? ['adaptive'] : [])]" :key="r" :class="{ selected: input.aspect_ratio === r }" @click="input.aspect_ratio = r"><span class="ratio-shape" :style="{ aspectRatio: r.replace(':', '/') }" />{{ r }}</button></div></label>
              <div class="section-heading"><h3>参考素材</h3><button @click="openLibrary('reference')">＋ 从素材库添加</button></div><p class="hint">图片 {{ input.images.length }}/{{ model?.max_images || 0 }} · 视频 {{ input.videos.length }}/{{ model?.max_videos || 0 }} · 音频 {{ input.audios.length }}/{{ model?.max_audios || 0 }}</p>
              <div class="asset-strip"><div v-for="id in input.images" :key="id" class="asset-thumb"><img :src="assetURL(id)" alt="参考图片" /><button aria-label="移除参考图" @click="input.images = input.images.filter(x => x !== id)">×</button></div><div v-for="id in [...input.videos, ...input.audios]" :key="id" class="media-chip">{{ assets.find(a => a.id === id)?.name || '媒体素材' }}<button @click="input.videos = input.videos.filter(x => x !== id); input.audios = input.audios.filter(x => x !== id)">×</button></div></div>
              <p v-if="singleError && model" class="hint" role="status">{{ singleError }}</p><footer class="generate-footer"><div><small>预计冻结</small><strong>{{ estimatedSingle > 0 ? money(estimatedSingle) : '请选择有效参数' }}</strong></div><button class="primary" :disabled="!canSubmit" @click="prepareSubmit()">{{ busy ? '正在报价…' : '生成视频 →' }}</button></footer>
            </div>
            <template v-else>
              <div class="canvas-toolbar"><div><h2>分镜画布</h2><span>{{ project.document.shots.length }} 个镜头 · {{ project.document.shots.reduce((n, s) => n + s.duration, 0) }} 秒</span></div><div><button aria-label="缩小" @click="zoom = Math.max(.6, zoom - .1)">−</button><button @click="zoom = 1">{{ Math.round(zoom * 100) }}%</button><button aria-label="放大" @click="zoom = Math.min(1.4, zoom + .1)">＋</button><button @click="addShot">添加镜头</button></div></div>
              <div class="story-canvas"><div v-if="!project.document.shots.length" class="canvas-empty"><div class="empty-icon">▧</div><h3>故事从第一镜开始</h3><p>在左侧写大纲并拆分剧本，或手动添加镜头。</p><button @click="addShot">＋ 添加第一个镜头</button></div><div class="shot-cards" :style="{ zoom }">
                <article v-for="(s, i) in project.document.shots" :key="s.id" class="shot-card" :class="{ selected: selectedShot === s.id }" @click="selectedShot = s.id">
                  <header><span class="shot-number">{{ String(i + 1).padStart(2, '0') }}</span><strong>{{ s.title }}</strong><span class="status" :class="taskForShot(s)?.status">{{ state(taskForShot(s)) }}</span></header>
                  <div class="shot-preview"><video v-if="taskForShot(s)?.outputs?.[0]" :src="safeURL(taskForShot(s)!.outputs![0]!)" controls preload="metadata" /><img v-else-if="s.first_frame" :src="assetURL(s.first_frame)" alt="构图参考" /><span v-else>镜头 {{ i + 1 }} · {{ s.duration }}s</span></div>
                  <textarea v-model="s.prompt" rows="4" :maxlength="model?.max_prompt_chars || 7000" aria-label="镜头提示词" placeholder="描述这一镜的画面与动作" />
                  <div class="shot-card-bottom"><input v-model.number="s.duration" type="number" :min="model?.min_seconds || 4" :max="model?.max_seconds || 15" aria-label="镜头时长" /><span>秒</span><button aria-label="上移镜头" @click.stop="moveShot(i, -1)">↑</button><button aria-label="下移镜头" @click.stop="moveShot(i, 1)">↓</button><button aria-label="删除镜头" @click.stop="removeShot(s)">删除</button></div>
                  <p v-if="shotError(s)" class="task-error">{{ shotError(s) }}</p><p v-if="taskForShot(s)?.error" class="task-error">{{ taskForShot(s)?.error }}</p>
                  <button class="primary full" :disabled="busy || batchBusy || !s.prompt.trim() || (!!taskForShot(s) && !taskForShot(s)?.settled)" @click.stop="prepareSubmit([s])">{{ taskForShot(s)?.settled ? '重新生成' : '生成这一镜' }}</button>
                </article>
              </div></div>
              <footer class="batch-footer"><div><strong>批量生成</strong><small>跳过已完成与处理中的镜头；整批入库，逐镜结算。</small></div><span v-if="batchBusy">正在保存整批任务…</span><button v-else class="primary" :disabled="busy || !project.document.shots.length" @click="prepareSubmit(project.document.shots.filter(s => !taskForShot(s) || taskForShot(s)?.status === 'failed'))">{{ busy ? '核对报价…' : '生成待处理镜头' }}</button></footer>
            </template>
          </main>
          <aside v-if="mode === 'story'" class="assets-panel">
            <div class="panel-title"><h2>资产与细节</h2><span>跨镜头复用</span></div>
            <h3>全剧角色参考</h3><p class="hint">最多5张，会随每个镜头提交，并计入该镜头的参考图和素材总数。</p><div class="asset-strip"><div v-for="id in project.document.character_images" :key="id" class="asset-thumb"><img :src="assetURL(id)" alt="角色参考" /><button @click="project.document.character_images = project.document.character_images.filter(x => x !== id)">×</button></div></div><button class="full dashed" @click="openLibrary('characters')">＋ 添加角色参考</button>
            <template v-if="shot"><p class="hint">本镜实际参考：图片 {{ shotInput(input, shot, project.document.character_images).images.length }}/{{ model?.max_images || 0 }} · 视频 {{ shot.videos?.length || 0 }}/{{ model?.max_videos || 0 }} · 音频 {{ shot.audios?.length || 0 }}/{{ model?.max_audios || 0 }}</p><h3>本镜构图参考</h3><p class="hint">作为普通参考图提交，不是强制首帧；计入参考图总数。</p><img v-if="shot.first_frame" class="first-frame" :src="assetURL(shot.first_frame)" alt="本镜构图参考" /><div class="split-buttons"><button @click="openLibrary('first')">选择构图参考</button><button v-if="shot.first_frame" @click="shot.first_frame = ''">移除</button></div><label>镜头名称<input v-model="shot.title" maxlength="120" /></label><label>运镜<input v-model="shot.camera" placeholder="例如：缓慢推进、固定机位" /></label><label>对白<textarea v-model="shot.dialogue" rows="2" /></label><label>声音<textarea v-model="shot.sound" rows="2" /></label><button class="full dashed" @click="openLibrary('reference')">添加本镜参考素材</button><div class="asset-strip"><div v-for="id in shot.images" :key="id" class="asset-thumb"><img :src="assetURL(id)" alt="镜头参考" /><button @click="shot.images = shot.images.filter(x => x !== id)">×</button></div></div><div v-for="kind in (['videos', 'audios'] as const)" :key="kind" class="asset-strip"><div v-for="id in shot[kind] || []" :key="id" class="media-chip">{{ assets.find(a => a.id === id)?.name || (kind === 'videos' ? '参考视频' : '参考音频') }}<button @click="shot[kind] = shot[kind]?.filter(x => x !== id)">×</button></div></div></template>
            <h3>全剧输出设置</h3><label>清晰度<select v-model="input.resolution"><option v-for="r in resolutions" :key="r" :value="r">{{ r.toUpperCase() }}</option></select></label><label>画幅<select v-model="input.aspect_ratio"><option v-for="r in ['16:9', '9:16', '1:1', '4:3', '3:4', '21:9', ...(model?.adaptive ? ['adaptive'] : [])]" :key="r">{{ r }}</option></select></label><label v-if="modelID !== 'MiniMax-H3'"><input v-model="input.generate_audio" type="checkbox" :disabled="!model?.audio" /> 生成声音</label>
            <h3>成片 · {{ completedShots.length }} 镜</h3><p class="hint">点击播放或下载各镜头；导出剧本可同时保存视频地址。</p><button v-for="x in completedShots" :key="x.shot.id" class="finished-shot" @click="preview = x.task || null">▶ {{ x.shot.title }}</button><button v-if="completedShots.length" class="full" @click="exportProject">导出剧本与成片清单</button>
          </aside>
          <aside v-if="historyOpen" class="history-panel"><div class="panel-title"><h2>历史记录</h2><span>{{ allTasks.length }}</span></div><div class="history-tabs"><button v-for="f in [{ id: 'all', label: '全部' }, { id: 'running', label: '进行中' }, { id: 'completed', label: '完成' }, { id: 'failed', label: '失败' }]" :key="f.id" :class="{ selected: filter === f.id }" @click="filter = f.id">{{ f.label }}</button></div><p v-if="!visibleTasks.length" class="hint">这里会保存你的视频生成记录。</p><article v-for="t in visibleTasks" :key="t.id" class="history-card"><header><strong>{{ t.request.model }}</strong><span class="status" :class="t.status">{{ state(t) }}</span></header><small>{{ new Date(t.created_at).toLocaleString() }} · {{ t.request.input.duration }}s · {{ t.request.input.resolution }}</small><p>{{ t.request.input.prompt }}</p><video v-if="t.outputs?.[0]" :src="safeURL(t.outputs[0])" controls preload="metadata" /><p v-if="t.error" class="task-error">{{ t.error }}</p><footer><span>{{ t.settled ? `实扣 ${money(t.cost)}` : `冻结 ${money(t.hold)}` }}</span><button v-if="t.status === 'pending'" @click="cancelTask(t)">取消排队</button><button v-if="t.status === 'completed'" @click="preview = t">查看 / 下载</button></footer></article><button v-if="allTasks.length >= 50" class="full" @click="moreHistory">加载更早记录</button></aside>
        </div>
      </template>
      <div v-if="confirming" class="modal-backdrop" @click.self="confirming = null"><section class="studio-modal" role="dialog" aria-modal="true" aria-label="确认视频费用"><h2>确认生成</h2><p>即将提交 {{ confirming.requests.length }} 个视频任务，冻结站内余额 <strong>{{ money(confirming.total) }}</strong>。</p><p>成功后结算，明确失败退回；上游结果不确定时保留冻结并由管理员核对。生成期间请勿重复提交。</p><div class="modal-actions"><button @click="confirming = null">返回修改</button><button class="primary" @click="confirmSubmit">确认费用并生成</button></div></section></div>
      <div v-if="showLibrary" class="modal-backdrop" @click.self="showLibrary = false"><section class="studio-modal library-modal" role="dialog" aria-modal="true" aria-label="素材库"><header class="panel-title"><h2>我的素材库</h2><button @click="showLibrary = false">关闭</button></header><p class="hint">角色 / 构图参考请选择图片；文件保存在本站，不依赖容易过期的外部图片地址。单文件 30MB，个人总量 200MB。</p><label class="file-button primary">{{ uploading ? '上传中…' : '＋ 上传素材' }}<input :disabled="uploading" type="file" accept="image/png,image/jpeg,image/webp,image/gif,video/mp4,video/webm,audio/mpeg,audio/wav,audio/ogg" @change="upload" /></label><div class="library-grid"><article v-for="a in libraryAssets" :key="a.id"><button class="library-select" @click="chooseAsset(a)"><img v-if="a.mime.startsWith('image/')" :src="assetURL(a.id)" :alt="a.name" /><span v-else>{{ a.mime.startsWith('video/') ? '▶ 视频' : '♫ 音频' }}</span><small>{{ a.name }}</small></button><button @click="deleteAsset(a)">删除</button></article></div><p v-if="!libraryAssets.length" class="empty-state">暂无符合类型的素材，请先上传。</p></section></div>
      <div v-if="preview" class="modal-backdrop" @click.self="preview = null"><section class="studio-modal preview-modal" role="dialog" aria-modal="true" aria-label="视频预览"><header class="panel-title"><h2>{{ preview.request.model }}</h2><button @click="preview = null">关闭</button></header><video v-if="preview.outputs?.[0]" :src="safeURL(preview.outputs[0])" controls autoplay /><p>{{ preview.request.input.prompt }}</p><a v-for="(url, i) in preview.outputs" :key="url" :href="safeURL(url)" target="_blank" rel="noopener noreferrer" class="primary file-button">打开 / 下载视频 {{ i + 1 }}</a><small>视频由上游外链提供，链接可能过期，请及时下载保存。若浏览器直接播放，可在播放器菜单选择下载。</small></section></div>
    </div>
  </AppLayout>
</template>

<style scoped>
.video-studio{--studio-text:#172033;--studio-bg:#fff;--studio-line:#e5eaf2;color:var(--studio-text);background:var(--studio-bg);border:1px solid var(--studio-line);border-radius:18px;overflow:hidden;min-height:80vh}.studio-top{display:flex;align-items:center;justify-content:space-between;padding:24px 28px;gap:12px;border-bottom:1px solid var(--studio-line)}.eyebrow{font-size:10px;letter-spacing:2px;color:#64748b}h1{font-size:23px;font-weight:700;letter-spacing:-.5px;margin-top:3px}h1 span{font-size:12px;font-weight:400;letter-spacing:0;color:#94a3b8;margin-left:12px}.top-actions,.studio-toolbar,.panel-title,.section-heading,.canvas-toolbar,.shot-card header,.shot-card-bottom,.generate-footer,.batch-footer,.history-card header,.history-card footer,.modal-actions{display:flex;align-items:center;justify-content:space-between;gap:10px}.top-actions{font-size:12px}.wallet{background:#f1f5f9;padding:8px 12px;border-radius:20px}.studio-toolbar{padding:12px 24px;border-bottom:1px solid var(--studio-line)}nav{display:flex;background:#f1f5f9;border-radius:10px;padding:3px;gap:3px}button,.file-button{padding:8px 12px;border:1px solid var(--studio-line);border-radius:8px;font-size:12px;line-height:1.4;cursor:pointer;background:var(--studio-bg);transition:background .15s}button:hover{background:#eff6ff}button:disabled{opacity:.45;cursor:not-allowed}nav button{background:transparent;border-color:transparent}nav .active{background:white;box-shadow:0 1px 4px #0f172a15;color:#2563eb;font-weight:600}.primary{background:#2563eb!important;color:#fff!important;border-color:#2563eb!important}.full{width:100%}.dashed{border-style:dashed;color:#64748b}.model-bar{display:grid;grid-template-columns:minmax(180px,1fr) minmax(180px,1fr) auto;gap:20px;padding:20px 24px;border-bottom:1px solid var(--studio-line)}label{display:block;font-size:12px;font-weight:550;color:#475569}input:not([type=range]):not([type=checkbox]),select,textarea{display:block;width:100%;border:1px solid #dce3ee;border-radius:9px;padding:10px 12px;margin-top:7px;background:var(--studio-bg);color:var(--studio-text);font-size:13px;outline:none}textarea{resize:vertical;line-height:1.8}input:focus,select:focus,textarea:focus{border-color:#7da8fa;box-shadow:0 0 0 3px #2563eb0a}.model-price{min-width:160px;display:flex;flex-direction:column;justify-content:center;gap:4px}.model-price span,.model-price small,.hint,small{font-size:11px;color:#8895a8}.model-price strong{font-size:17px;font-weight:650}.workspace{display:grid;grid-template-columns:minmax(0,1fr)}.workspace.with-history{grid-template-columns:minmax(0,1fr) 300px}.workspace.story{grid-template-columns:245px minmax(300px,1fr) 260px}.workspace.story.with-history{grid-template-columns:230px minmax(300px,1fr) 240px 260px}.creation-panel{min-width:0}.single-editor{padding:28px;max-width:1000px;margin:auto}.section-heading{margin-bottom:20px}.section-heading h2,h2{font-size:15px;font-weight:650}.section-heading span{font-size:12px;color:#94a3b8}h3{font-size:13px;font-weight:600}.main-prompt{font-size:15px;min-height:170px;background:#fafbfd}.form-row{display:flex;gap:50px;margin:26px 0}.form-row>label{flex:1}.form-row strong{float:right;color:#2563eb}.form-row input[type=range]{width:100%;accent-color:#2563eb;margin:16px 0 6px}.chips{display:flex;gap:8px;margin-top:12px}.chips button{border-radius:20px;min-width:62px}.selected{border-color:#6089f9!important;background:#eef3ff!important;color:#2456c9}.ratio-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:10px;margin:12px 0 28px}.ratio-grid button{height:70px;display:flex;align-items:center;justify-content:center;gap:16px}.ratio-shape{display:block;width:25px;max-height:30px;border:1px solid currentColor;opacity:.7;border-radius:2px}.asset-strip{display:flex;gap:8px;flex-wrap:wrap;margin:12px 0}.asset-thumb{position:relative;width:68px;height:68px;border:1px solid var(--studio-line);border-radius:10px;overflow:hidden}.asset-thumb img{width:100%;height:100%;object-fit:cover}.asset-thumb button{position:absolute;right:2px;top:2px;padding:0 5px;background:#fff;border:0;font-size:14px}.media-chip{background:#f1f5f9;border-radius:8px;padding:6px;font-size:12px}.generate-footer{border-top:1px solid var(--studio-line);padding-top:24px;margin-top:30px}.generate-footer>div{display:flex;flex-direction:column;gap:4px}.generate-footer strong{font-size:24px;font-weight:650}.generate-footer .primary{min-width:200px;padding:12px}.script-panel,.assets-panel,.history-panel{padding:20px 16px;border-right:1px solid var(--studio-line);min-width:0;max-height:950px;overflow:auto}.script-panel{background:#fdfefe}.script-panel>input,.script-panel>textarea{margin:12px 0}.planner-settings{border:1px solid var(--studio-line);border-radius:9px;padding:10px;margin:12px 0;font-size:12px}.planner-settings label{margin:10px 0}.planner-settings small{display:block;line-height:1.8}.shot-list-title{display:flex;justify-content:space-between;align-items:center;margin:22px 0 10px}.shot-list-item{display:flex;align-items:center;text-align:left;width:100%;gap:10px;margin:6px 0;border-color:transparent}.shot-number{font-size:12px;color:#7b8aa1;font-variant-numeric:tabular-nums}.shot-list-item small{display:block;margin-top:4px}.project-actions{display:flex;gap:5px;flex-wrap:wrap;margin-top:20px}.project-actions>span{width:100%;font-size:10px;color:#94a3b8}.project-actions button,.project-actions label{padding:6px 8px}.canvas-toolbar{padding:17px 20px;border-bottom:1px solid var(--studio-line)}.canvas-toolbar span{font-size:11px;color:#94a3b8}.canvas-toolbar>div:last-child{display:flex;gap:4px}.story-canvas{min-height:570px;overflow:auto;background-color:#fafbfe;background-image:linear-gradient(#e5eaf280 1px,transparent 1px),linear-gradient(90deg,#e5eaf280 1px,transparent 1px);background-size:28px 28px;padding:24px}.canvas-empty{text-align:center;padding:160px 12px;color:#64748b}.empty-icon{font-size:38px;color:#a9bad2;margin-bottom:15px}.canvas-empty p{font-size:12px;margin:10px 0 20px}.shot-cards{display:flex;align-items:flex-start;gap:22px;width:max-content;min-width:100%;flex-wrap:wrap}.shot-card{background:white;border:1px solid #e1e7f0;border-radius:13px;padding:14px;width:280px;box-shadow:0 3px 15px #15264b05;color:#172033}.shot-card header{margin-bottom:12px;flex-wrap:wrap}.shot-card header strong{font-size:12px;flex:1}.shot-preview{height:145px;border-radius:8px;background:#f1f5f9;display:flex;align-items:center;justify-content:center;color:#a0aabc;font-size:12px;overflow:hidden}.shot-preview video,.shot-preview img{width:100%;height:100%;object-fit:contain}.shot-card textarea{background:#fff;color:#172033}.shot-card-bottom{margin:12px 0;gap:4px;justify-content:flex-start}.shot-card-bottom input{width:55px!important;margin:0!important;padding:5px!important}.shot-card-bottom button{padding:4px 6px}.shot-card-bottom span{font-size:11px;color:#64748b;margin-right:auto}.batch-footer{padding:18px 20px;border-top:1px solid var(--studio-line)}.batch-footer small{display:block;margin-top:5px}.assets-panel{border-left:1px solid var(--studio-line);border-right:0;background:#fbfcfe}.assets-panel h3{margin:25px 0 10px}.assets-panel label{margin:12px 0}.assets-panel .hint{line-height:1.8}.first-frame{width:100%;max-height:150px;object-fit:contain;border-radius:8px;background:#edf1f7}.split-buttons{display:flex;gap:8px;margin-top:8px}.finished-shot{display:block;width:100%;text-align:left;margin:7px 0}.history-panel{border-left:1px solid var(--studio-line);border-right:0;background:#f5f7fb}.history-tabs{display:flex;gap:5px;margin:18px 0}.history-tabs button{padding:5px 9px;border-radius:20px;font-size:11px}.history-card{background:var(--studio-bg);border:1px solid var(--studio-line);border-radius:12px;padding:13px;margin:12px 0}.history-card header{align-items:flex-start}.history-card strong{font-size:12px}.history-card>small{display:block;margin-top:9px;font-size:10px}.history-card p{font-size:11px;line-height:1.7;margin:9px 0;white-space:pre-wrap;max-height:70px;overflow:auto}.history-card video{width:100%;max-height:190px;border-radius:8px}.history-card footer{margin-top:9px;font-size:11px;color:#64748b}.history-card footer button{padding:5px 7px;font-size:10px}.status{font-size:9px;white-space:normal;border-radius:5px;background:#eef2f7;color:#64748b;padding:3px 5px}.status.completed{color:#168464;background:#eaf8f1}.status.failed{color:#c54848;background:#fff0f0}.status.in_progress,.status.queued{color:#3865ce;background:#edf3ff}.status.needs_review{color:#b7791f;background:#fff7e4}.task-error{color:#bc5151!important;font-size:11px;line-height:1.7}.studio-message,.setup-notice{padding:12px 20px;margin:12px 20px;border-radius:9px;font-size:12px;background:#fff6e8;color:#91632c;line-height:1.8}.studio-message{display:flex;justify-content:space-between;gap:15px}.studio-message button{padding:0 5px;background:transparent;border:0}.setup-notice a{color:#2563eb;margin-left:12px}.model-limits{padding:0 20px 12px}.empty-state{text-align:center;padding:60px;color:#94a3b8;font-size:13px}.modal-backdrop{position:fixed;inset:0;z-index:100;background:#0f172a88;backdrop-filter:blur(4px);display:flex;align-items:center;justify-content:center;padding:25px}.studio-modal{background:var(--studio-bg);border-radius:18px;padding:26px;max-width:520px;width:100%;max-height:88vh;overflow:auto;box-shadow:0 20px 70px #0003}.studio-modal p{font-size:13px;line-height:1.8;margin:18px 0}.studio-modal h2{font-size:19px}.modal-actions{justify-content:flex-end;margin-top:20px}.file-button{display:inline-flex;align-items:center;justify-content:center;font-weight:400}.file-button input{display:none!important}.library-modal{max-width:850px}.library-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(125px,1fr));gap:14px;margin-top:24px}.library-grid article{border:1px solid var(--studio-line);border-radius:10px;padding:6px}.library-select{display:block;width:100%;padding:0;border:0}.library-select img,.library-select>span{width:100%;height:100px;object-fit:cover;border-radius:6px;display:flex;align-items:center;justify-content:center;background:#eff3f8}.library-select small{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;padding:6px}.library-grid article>button:last-child{font-size:10px;padding:4px 7px}.preview-modal{max-width:900px}.preview-modal video{width:100%;max-height:60vh;margin-top:16px;border-radius:10px;background:#111}.preview-modal>small{display:block;margin-top:12px}
:global(.dark) .video-studio{--studio-text:#e0e8f5;--studio-bg:#141e30;--studio-line:#29364c}:global(.dark) .wallet,:global(.dark) nav,:global(.dark) .history-panel,:global(.dark) .assets-panel,:global(.dark) .script-panel{background:#172237}:global(.dark) label{color:#b4c3da}:global(.dark) .story-canvas{background-color:#111a2a;background-image:linear-gradient(#26344a55 1px,transparent 1px),linear-gradient(90deg,#26344a55 1px,transparent 1px)}:global(.dark) .main-prompt{background:#111a2a}:global(.dark) .studio-message,:global(.dark) .setup-notice{background:#392e20;color:#efc98e}
@media(max-width:1550px){.workspace.story.with-history{grid-template-columns:220px minmax(300px,1fr) 230px}.story.with-history .history-panel{grid-column:1/-1;max-height:420px;display:flex;gap:14px;flex-wrap:wrap}.story.with-history .history-panel .history-card{width:250px}.story.with-history .history-panel .panel-title{width:100%}.story.with-history .history-tabs{width:100%;margin:0}}
@media(max-width:1000px){.workspace.story,.workspace.story.with-history{grid-template-columns:210px minmax(0,1fr)}.assets-panel{grid-column:1/-1;max-height:none;display:block}.workspace.with-history{grid-template-columns:minmax(0,1fr)}.history-panel{grid-column:1/-1;max-height:600px}.model-bar{grid-template-columns:1fr 1fr}.model-price{grid-column:1/-1;flex-direction:row;align-items:center;justify-content:flex-start}.model-price small{margin-left:auto}h1 span{display:none}.shot-cards{flex-wrap:nowrap}.canvas-toolbar{flex-wrap:wrap}}
@media(max-width:640px){.studio-top{padding:18px;align-items:flex-start}.top-actions{flex-direction:column;align-items:flex-end}.studio-toolbar{padding:12px}.model-bar{padding:16px;gap:12px;grid-template-columns:1fr}.model-price{flex-wrap:wrap}.model-price small{width:100%;margin:0}.workspace.story,.workspace.story.with-history{display:flex;flex-direction:column}.script-panel{max-height:none;border-right:0;border-bottom:1px solid var(--studio-line)}.single-editor{padding:18px}.section-heading{flex-wrap:wrap}.form-row{gap:20px}.ratio-grid{grid-template-columns:repeat(3,1fr)}.ratio-grid button{gap:7px}.generate-footer .primary{min-width:150px}.batch-footer{flex-wrap:wrap}.history-panel{max-height:none}.studio-modal{padding:20px}.story-canvas{min-height:350px}.canvas-empty{padding:80px 10px}.historyOpen .history-panel{width:100%}.studio-top h1{font-size:20px}}
</style>
