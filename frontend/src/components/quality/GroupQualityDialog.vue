<template>
  <BaseDialog :show="show" :title="`${t('groupQuality.title')} · ${config.group_name}`" width="extra-wide" @close="emit('close')">
    <div class="mb-4 rounded-xl bg-amber-50 p-3 text-xs text-amber-800 dark:bg-amber-950/30 dark:text-amber-200">{{ t('groupQuality.warning') }}</div>
    <p class="mb-4 text-xs text-gray-500">{{ t('groupQuality.direct') }}</p>
    <div class="grid min-h-[420px] gap-5 lg:grid-cols-[260px_minmax(0,1fr)]">
      <aside class="space-y-3">
        <label class="block text-xs text-gray-500">{{ t('groupQuality.models') }}</label>
        <Select v-model="model" :options="modelOptions" :disabled="starting" />
        <button v-if="canRun" class="btn btn-primary w-full" :disabled="!canStart" @click="start">{{ starting ? t('groupQuality.pending') : t('groupQuality.start') }}</button>
        <p v-if="remaining > 0" class="text-xs text-amber-600">{{ t('groupQuality.remaining', { seconds: remaining }) }}</p>
        <p v-if="!supported" class="text-xs text-gray-500">{{ t('groupQuality.unsupported') }}</p>
        <p class="text-xs leading-relaxed text-gray-500">{{ t('groupQuality.coolHint') }}</p>
        <label class="block text-xs text-gray-500">{{ t('groupQuality.pollInterval') }}</label>
        <Select v-model="pollSeconds" :options="pollOptions" />
        <p class="text-xs text-gray-500">{{ t('groupQuality.pollHint') }}</p>
        <div class="flex items-center justify-between text-xs text-gray-500">
          <span>{{ t('groupQuality.history') }}</span>
          <button class="btn btn-secondary btn-sm" :disabled="loading" @click="manualRefresh">{{ t('groupQuality.refresh') }}</button>
        </div>
        <p v-if="loading && !runs.length" class="text-xs text-gray-500">{{ t('groupQuality.pending') }}</p>
        <p v-else-if="!runs.length" class="text-xs text-gray-500">{{ t('groupQuality.noHistory') }}</p>
        <button v-for="run in runs" :key="run.id" class="w-full rounded-xl border p-3 text-left transition-colors dark:border-dark-600" :class="selectedId === run.id ? 'border-primary-400 bg-primary-50 dark:bg-primary-900/20' : 'border-gray-200 hover:bg-gray-50 dark:hover:bg-dark-700'" @click="selectRun(run)">
          <span class="badge" :class="run.status === 'failed' ? 'badge-danger' : run.status === 'running' ? 'badge-warning' : 'badge-success'">{{ statusLabel(run.status) }}</span>
          <p class="mt-2 text-xs text-gray-500">{{ date(run.started_at) }}</p>
          <p class="mt-1 text-xs text-gray-500">{{ run.automatic ? t('groupQuality.scheduledRun') : t('groupQuality.manualRun') }}</p>
          <p class="mt-1 text-xs text-gray-500">{{ (run.duration_ms / 1000).toFixed(1) }}s · {{ run.response_length.toLocaleString() }} {{ t('groupQuality.length') }}</p>
        </button>
      </aside>
      <section class="min-w-0 space-y-3">
        <div v-if="selected" class="flex flex-wrap items-center justify-between gap-2 text-xs text-gray-500">
          <span>{{ selected.model }} · {{ t('groupQuality.duration') }} {{ (selected.duration_ms / 1000).toFixed(1) }}s</span>
          <div v-if="selected.source" class="flex gap-2">
            <button class="btn btn-secondary btn-sm" @click="showSource = !showSource">{{ showSource ? t('groupQuality.preview') : t('groupQuality.viewSource') }}</button>
            <button class="btn btn-secondary btn-sm" @click="copySource">{{ t('groupQuality.copy') }}</button>
          </div>
        </div>
        <div v-if="selected && admin" class="flex flex-wrap gap-3 text-xs text-gray-500">
          <span>{{ t('groupQuality.selectedAccount') }}: {{ selected.account_id ?? '—' }}</span>
          <span>{{ t('groupQuality.mappedModel') }}: {{ selected.upstream_model || '—' }}</span>
        </div>
        <div v-if="selected" class="flex flex-wrap gap-3 text-xs text-gray-500">
          <span>{{ t('groupQuality.started') }}: {{ date(selected.started_at) }}</span>
          <span>{{ t('groupQuality.finished') }}: {{ selected.finished_at ? date(selected.finished_at) : '—' }}</span>
          <span>{{ t('groupQuality.usage') }}: {{ selected.input_tokens }} / {{ selected.output_tokens }}</span>
          <span v-if="selected.prompt_version">{{ selected.prompt_version }}</span>
        </div>
        <div v-if="error" role="alert" class="rounded-xl bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">{{ error }}</div>
        <div v-if="selected?.error_message" class="rounded-xl bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-950/30 dark:text-amber-200">{{ selected.error_message }}</div>
        <pre v-if="showSource && selected?.source" class="h-[min(60vh,640px)] overflow-auto rounded-xl bg-gray-950 p-4 text-xs text-gray-100">{{ selected.source }}</pre>
        <iframe v-else-if="preview" :key="selected?.id" :srcdoc="preview" sandbox="allow-scripts" referrerpolicy="no-referrer" :title="t('groupQuality.preview')" class="h-[min(60vh,640px)] min-h-[360px] w-full rounded-xl border border-gray-200 bg-white dark:border-dark-700"></iframe>
        <div v-else class="flex h-[360px] items-center justify-center rounded-xl border border-dashed border-gray-200 text-sm text-gray-400 dark:border-dark-600">{{ selected?.status === 'running' ? t('groupQuality.running') : t('groupQuality.noArtifact') }}</div>
        <p class="text-xs text-gray-500">{{ t('groupQuality.sourceHint') }}</p>
      </section>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { qualityDetail, qualityHistory, startQualityTest, type QualityConfig, type QualityModel, type QualityRun } from '@/api/groupQuality'
import { qualityPreviewDocument } from '@/features/group-quality/preview'

const props = defineProps<{ show: boolean; config: QualityConfig; admin: boolean }>()
const emit = defineEmits<{ (e: 'close'): void }>()
const { t } = useI18n()
const app = useAppStore()
const model = ref<QualityModel>('gpt-6-astra')
const runs = ref<QualityRun[]>([])
const selected = ref<QualityRun | null>(null)
const selectedId = ref<number | null>(null)
const error = ref('')
const loading = ref(false)
const starting = ref(false)
const showSource = ref(false)
const now = ref(Date.now())
const pollValues = [0, 3, 5, 10, 30, 60]
const pollStorageKey = 'group-quality-poll-seconds'
function readPollSeconds() {
  try { const raw = localStorage.getItem(pollStorageKey); const value = raw === null ? 5 : Number(raw); return pollValues.includes(value) ? value : 5 }
  catch { return 5 }
}
const pollSeconds = ref(readPollSeconds())
const pollOptions = computed(() => pollValues.map(value => ({ value, label: value === 0 ? t('groupQuality.manualPoll') : t('groupQuality.pollSeconds', { seconds: value }) })))
let generation = 0
let historySequence = 0
let detailSequence = 0
let failures = 0
let historyController: AbortController | undefined
let detailController: AbortController | undefined
let timer: ReturnType<typeof setTimeout> | undefined
let clock: ReturnType<typeof setInterval> | undefined
const modelOptions = computed(() => props.config.models.map(value => ({ value, label: value })))
const supported = computed(() => props.config.available_models.includes(model.value))
const canRun = computed(() => props.config.enabled && (props.admin || props.config.customer_access === 'run'))
const remaining = computed(() => {
  const last = runs.value[0]
  if (!last || last.status === 'running') return 0
  return Math.max(0, Math.ceil((Date.parse(last.finished_at || last.deadline_at) + props.config.cooldown_seconds * 1000 - now.value) / 1000))
})
const canStart = computed(() => canRun.value && supported.value && !loading.value && !starting.value && remaining.value === 0 && !runs.value.some(r => r.status === 'running'))
const preview = computed(() => selected.value?.source ? qualityPreviewDocument(selected.value.source) : '')
const date = (s: string) => new Date(s).toLocaleString()
const statusLabel = (status: string) => t(`groupQuality.${status}`)

function stopRequests() {
  clearTimeout(timer)
  historySequence++; detailSequence++
  historyController?.abort(); detailController?.abort()
}
function terminalError(e: unknown) {
  const value = e as { status?: number; response?: { status?: number } }
  return [401, 403, 404].includes(value?.status ?? value?.response?.status ?? 0)
}
function schedule(token: number, retry = false) {
  clearTimeout(timer)
  if (token !== generation || !props.show || document.hidden || pollSeconds.value === 0) return
  if (retry && failures >= 3) { error.value += ` ${t('groupQuality.retryStopped')}`; return }
  const delay = retry ? Math.min(60000, pollSeconds.value * 1000 * 2 ** failures) : pollSeconds.value * 1000
  timer = setTimeout(() => void refresh(), delay)
}
async function selectRun(run: QualityRun): Promise<boolean> {
  if (!props.show) return false
  detailController?.abort()
  const sequence = ++detailSequence
  const controller = new AbortController()
  detailController = controller
  selectedId.value = run.id
  selected.value = run
  showSource.value = false
  const token = generation
  if (run.status !== 'completed') return true
  try {
    const detail = await qualityDetail(props.admin, run.id, controller.signal)
    if (token !== generation || sequence !== detailSequence || !props.show || controller.signal.aborted || selectedId.value !== run.id) return false
    selected.value = detail
    error.value = ''
    return true
  } catch (e) {
    if (token !== generation || sequence !== detailSequence || controller.signal.aborted || !props.show) return false
    error.value = extractApiErrorMessage(e)
    if (terminalError(e)) failures = 3
    return false
  }
}
async function refresh() {
  if (!props.show) return
  const token = generation
  const sequence = ++historySequence
  historyController?.abort()
  const controller = new AbortController()
  historyController = controller
  clearTimeout(timer)
  loading.value = true
  try {
    const result = await qualityHistory(props.admin, props.config.group_id, model.value, controller.signal)
    if (token !== generation || sequence !== historySequence || !props.show || controller.signal.aborted) return
    runs.value = result
    error.value = ''
    const current = result.find(r => r.id === selectedId.value) || result[0]
    let detailOK = true
    if (current && (current.id !== selectedId.value || selected.value?.status === 'running' || (current.status === 'completed' && !selected.value?.source))) detailOK = await selectRun(current)
    if (token !== generation || sequence !== historySequence || !props.show || controller.signal.aborted) return
    if (!detailOK) { failures++; schedule(token, true) }
    else { failures = 0; if (result.some(r => r.status === 'running')) schedule(token) }
  } catch (e) {
    if (token !== generation || sequence !== historySequence || !props.show || controller.signal.aborted) return
    error.value = extractApiErrorMessage(e)
    if (terminalError(e)) failures = 3
    else failures++
    schedule(token, true)
  } finally {
    if (token === generation && sequence === historySequence) loading.value = false
  }
}
function manualRefresh() { failures = 0; void refresh() }
async function start() {
  if (!canStart.value) return
  const token = generation
  starting.value = true
  error.value = ''
  try {
    const run = await startQualityTest(props.admin, props.config.group_id, model.value)
    if (token !== generation || !props.show) return
    selectedId.value = run.id
    selected.value = run
    showSource.value = false
    await refresh()
  } catch (e) {
    if (token === generation) error.value = extractApiErrorMessage(e)
  } finally {
    if (token === generation) starting.value = false
  }
}
async function copySource() {
  try { await navigator.clipboard.writeText(selected.value?.source || ''); app.showSuccess(t('groupQuality.copied')) }
  catch { app.showError(t('groupQuality.copyFailed')) }
}
watch(() => [props.show, props.config.group_id] as const, () => {
  generation++
  stopRequests(); failures = 0
  clearInterval(clock)
  runs.value = []; selected.value = null; selectedId.value = null; error.value = ''; loading.value = false; starting.value = false
  if (props.show) {
    model.value = props.config.models[0] || 'gpt-6-astra'
    now.value = Date.now()
    clock = setInterval(() => { now.value = Date.now() }, 1000)
    void refresh()
  }
}, { immediate: true })
watch(model, () => {
  generation++; stopRequests(); failures = 0; loading.value = false; starting.value = false; runs.value = []; selected.value = null; selectedId.value = null
  if (props.show) void refresh()
})
watch(pollSeconds, value => {
  if (!pollValues.includes(value)) { pollSeconds.value = 5; return }
  try { localStorage.setItem(pollStorageKey, String(value)) } catch { /* Private browsing may disable storage. */ }
  clearTimeout(timer)
  if (props.show && !loading.value && runs.value.some(r => r.status === 'running')) schedule(generation)
})
function onVisibility() {
  if (document.hidden) clearTimeout(timer)
  else if (props.show && pollSeconds.value > 0 && !loading.value && failures < 3 && (runs.value.some(r => r.status === 'running') || (selected.value?.status === 'completed' && !selected.value.source))) void refresh()
}
document.addEventListener('visibilitychange', onVisibility)
onBeforeUnmount(() => { generation++; stopRequests(); clearInterval(clock); document.removeEventListener('visibilitychange', onVisibility) })
</script>
