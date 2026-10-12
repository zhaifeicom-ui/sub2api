<template>
  <AppLayout>
    <div class="space-y-5">
      <header class="page-header flex flex-wrap items-center justify-between gap-3">
        <div><h1 class="page-title">{{ t('groupQuality.title') }}</h1><p class="page-description">{{ t('groupQuality.description') }}</p></div>
        <div class="flex gap-2">
          <button class="btn btn-secondary" :disabled="loading" @click="load">{{ t('groupQuality.refresh') }}</button>
          <button class="btn btn-primary" @click="openCreate">{{ t('groupQuality.add') }}</button>
        </div>
      </header>

      <div class="card flex flex-wrap items-start gap-3 border-blue-200 bg-blue-50 text-sm text-blue-900 dark:border-blue-900 dark:bg-blue-950/20 dark:text-blue-100">
        <Icon name="lightbulb" size="sm" class="mt-0.5 shrink-0" />
        <div><p>{{ t('groupQuality.channelKeyHint') }}</p><p class="mt-1 text-xs opacity-80">{{ t('groupQuality.availabilityHint') }} {{ t('groupQuality.autoCostHint') }}</p></div>
      </div>

      <SearchInput v-model="search" :placeholder="t('groupQuality.search')" class="max-w-sm" />
      <div v-if="error" role="alert" class="rounded-xl bg-red-50 p-3 text-red-700 dark:bg-red-950/30 dark:text-red-300">{{ error }}</div>
      <div class="table-container">
        <table class="table">
          <thead><tr><th>{{ t('groupQuality.channel') }}</th><th>{{ t('groupQuality.enabled') }}</th><th>{{ t('groupQuality.models') }}</th><th>{{ t('groupQuality.access') }}</th><th>{{ t('groupQuality.cooldown') }}</th><th class="text-right">{{ t('groupQuality.actions') }}</th></tr></thead>
          <tbody>
            <tr v-for="config in filtered" :key="config.monitor_id">
              <td><p class="font-medium">{{ config.monitor_name }}</p><p class="text-xs text-gray-400">{{ providerLabel(config.provider) }} · #{{ config.monitor_id }}</p></td>
              <td><span class="badge" :class="config.enabled ? 'badge-success' : 'badge-gray'">{{ config.enabled ? t('common.enabled') : t('common.disabled') }}</span></td>
              <td><div class="flex flex-wrap gap-1.5"><span v-for="model in config.models" :key="model" class="badge" :class="config.available_models.includes(model) ? 'badge-primary' : 'badge-warning'">{{ model }}</span></div></td>
              <td>{{ t(`groupQuality.${config.customer_access}`) }}</td>
              <td><p>{{ config.cooldown_seconds / 60 }} {{ t('groupQuality.minutes') }}</p><p class="mt-1 text-xs text-gray-500">{{ config.auto_enabled ? t('groupQuality.autoEvery', { minutes: (config.auto_interval_seconds || 600) / 60 }) : t('groupQuality.autoOff') }}</p></td>
              <td><div class="flex justify-end gap-2"><button class="btn btn-secondary btn-sm" @click="edit(config)">{{ t('groupQuality.edit') }}</button><button class="btn btn-primary btn-sm" :disabled="!config.enabled || !config.available_models.length" @click="target = config">{{ t('groupQuality.test') }}</button><button class="btn btn-ghost btn-sm text-red-600" @click="deleting = config">{{ t('common.delete') }}</button></div></td>
            </tr>
            <tr v-if="!filtered.length"><td colspan="6" class="py-10 text-center text-gray-400">{{ loading ? t('groupQuality.pending') : t('groupQuality.empty') }}</td></tr>
          </tbody>
        </table>
      </div>
    </div>

    <BaseDialog :show="!!draft" :title="`${draft?.version ? t('groupQuality.edit') : t('groupQuality.add')} · ${draft?.monitor_name || ''}`" @close="closeEdit">
      <form v-if="draft" id="quality-config-form" class="space-y-5" @submit.prevent="save">
        <div v-if="draft.version === 0">
          <label class="input-label" for="quality-monitor">{{ t('groupQuality.channel') }}</label>
          <Select id="quality-monitor" v-model="monitorIDValue" :options="monitorOptions" :placeholder="t('groupQuality.selectChannel')" :disabled="monitorsLoading" />
        </div>
        <div v-else class="rounded-xl bg-gray-50 px-3 py-2 text-sm dark:bg-dark-700"><span class="font-medium">{{ draft.monitor_name }}</span><span class="ml-2 text-xs text-gray-500">{{ providerLabel(draft.provider) }} · #{{ draft.monitor_id }}</span></div>
        <div class="flex items-center justify-between"><label class="input-label">{{ t('groupQuality.enabled') }}</label><Toggle v-model="draft.enabled" :aria-label="t('groupQuality.enabled')" /></div>
        <fieldset>
          <legend class="input-label">{{ t('groupQuality.models') }}</legend>
          <div v-if="selectedModels.length" class="mt-2 flex flex-wrap gap-3">
            <label v-for="model in selectedModels" :key="model" class="inline-flex items-center gap-2 text-sm"><input v-model="draft.models" type="checkbox" :value="model" class="checkbox" />{{ model }}</label>
          </div>
          <p v-else class="mt-2 text-xs text-gray-500">{{ t('groupQuality.noSupportedModels') }}</p>
          <p class="mt-1 text-xs text-gray-500">{{ t('groupQuality.modelSourceHint') }}</p>
        </fieldset>
        <div><label class="input-label">{{ t('groupQuality.access') }}</label><Select v-model="draft.customer_access" :options="accessOptions" /><p class="mt-2 text-xs text-gray-500">{{ t('groupQuality.customerHint') }}</p></div>
        <div><label for="quality-cooldown" class="input-label">{{ t('groupQuality.cooldown') }}</label><input id="quality-cooldown" v-model.number="cooldownMinutes" type="number" min="5" max="1440" step="1" class="input" required /></div>
        <div class="flex items-center justify-between"><label class="input-label">{{ t('groupQuality.autoDetection') }}</label><Toggle v-model="autoEnabled" :aria-label="t('groupQuality.autoDetection')" /></div>
        <div><label for="quality-auto-interval" class="input-label">{{ t('groupQuality.autoInterval') }}</label><div class="mb-2 flex flex-wrap gap-2"><button v-for="minutes in [10, 30, 60, 120, 360, 1440]" :key="minutes" type="button" class="btn btn-secondary btn-sm" @click="autoMinutes = minutes">{{ t('groupQuality.intervalMinutes', { minutes }) }}</button></div><input id="quality-auto-interval" v-model.number="autoMinutes" type="number" min="10" max="525600" step="1" class="input" required /></div>
        <div v-if="formError" role="alert" class="text-sm text-red-600">{{ formError }}</div>
      </form>
      <template #footer><button class="btn btn-secondary" :disabled="saving" @click="closeEdit">{{ t('groupQuality.cancel') }}</button><button type="submit" form="quality-config-form" class="btn btn-primary" :disabled="saving || monitorsLoading || !draft?.monitor_id">{{ t('groupQuality.save') }}</button></template>
    </BaseDialog>

    <ConfirmDialog :show="!!deleting" :title="t('groupQuality.deleteTitle')" :message="t('groupQuality.deleteMessage', { name: deleting?.monitor_name || '' })" :danger="true" :confirm-text="t('common.delete')" @cancel="deleting = null" @confirm="confirmDelete" />
    <GroupQualityDialog v-if="target" :show="true" :config="target" :admin="true" @close="target = null" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import GroupQualityDialog from '@/components/quality/GroupQualityDialog.vue'
import { QUALITY_MODELS, deleteQualityConfig, qualityConfigs, saveQualityConfig, type QualityConfig, type QualityModel } from '@/api/groupQuality'
import { list as listMonitors, type ChannelMonitor } from '@/api/admin/channelMonitor'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const app = useAppStore()
const configs = ref<QualityConfig[]>([])
const monitors = ref<ChannelMonitor[]>([])
const search = ref('')
const loading = ref(false)
const monitorsLoading = ref(false)
const error = ref('')
const draft = ref<QualityConfig | null>(null)
const target = ref<QualityConfig | null>(null)
const deleting = ref<QualityConfig | null>(null)
const saving = ref(false)
const formError = ref('')
const cooldownMinutes = ref(10)
const autoMinutes = ref(10)
const autoEnabled = computed({ get: () => draft.value?.auto_enabled ?? false, set: value => { if (draft.value) draft.value.auto_enabled = value } })
const accessOptions = computed(() => ['hidden', 'view', 'run'].map(value => ({ value, label: t(`groupQuality.${value}`) })))
const availableForMonitor = (monitor?: ChannelMonitor) => {
  if (!monitor || monitor.provider !== 'openai' || monitor.check_mode === 'quota') return []
  const models = new Set([monitor.primary_model, ...monitor.extra_models])
  return QUALITY_MODELS.filter(model => models.has(model))
}
const monitorOptions = computed(() => monitors.value
  .filter(m => m.enabled && availableForMonitor(m).length && !configs.value.some(config => config.monitor_id === m.id))
  .map(m => ({ value: String(m.id), label: `${m.name} · ${m.group_name || t(`monitorCommon.providers.${m.provider}`)} · ${availableForMonitor(m).join(', ')}` })))
const monitorIDValue = computed({
  get: () => draft.value?.monitor_id ? String(draft.value.monitor_id) : '',
  set: (value: string) => {
    if (!draft.value || draft.value.version !== 0) return
    const monitor = monitors.value.find(item => item.id === Number(value))
    if (!monitor) return
    draft.value.monitor_id = monitor.id
    draft.value.monitor_name = monitor.name
    draft.value.provider = monitor.provider
    draft.value.available_models = availableForMonitor(monitor)
    draft.value.models = [...draft.value.available_models] as QualityModel[]
  },
})
const selectedModels = computed(() => draft.value?.available_models || [])
const filtered = computed(() => configs.value.filter(c => `${c.monitor_name} ${c.provider}`.toLowerCase().includes(search.value.toLowerCase())))
function providerLabel(provider: string) { return t(`monitorCommon.providers.${provider}`) }

async function load() {
  loading.value = true; error.value = ''
  try { configs.value = await qualityConfigs(true) } catch (e) { error.value = extractApiErrorMessage(e) }
  finally { loading.value = false }
}
async function loadMonitors() {
  monitorsLoading.value = true
  try {
    const collected: ChannelMonitor[] = []
    for (let page = 1; page <= 10; page++) {
      const result = await listMonitors({ page, page_size: 200 })
      collected.push(...result.items)
      if (collected.length >= result.total || result.items.length === 0) break
    }
    monitors.value = collected
  } catch (e) { formError.value = extractApiErrorMessage(e) }
  finally { monitorsLoading.value = false }
}
function openCreate() {
  formError.value = ''
  cooldownMinutes.value = 10
  autoMinutes.value = 10
  draft.value = { monitor_id: 0, monitor_name: '', provider: '', enabled: true, models: [], customer_access: 'hidden', cooldown_seconds: 600, auto_enabled: false, auto_interval_seconds: 600, version: 0, available_models: [] }
  if (!monitors.value.length) void loadMonitors()
}
function closeEdit() { if (!saving.value) { draft.value = null; formError.value = '' } }
function edit(config: QualityConfig) {
  draft.value = { ...config, models: config.models.filter(model => config.available_models.includes(model)), available_models: [...config.available_models] }
  cooldownMinutes.value = config.cooldown_seconds / 60
  autoMinutes.value = (config.auto_interval_seconds || 600) / 60
  formError.value = ''
}
async function save() {
  if (!draft.value || saving.value) return
  if (!draft.value.monitor_id) { formError.value = t('groupQuality.selectChannel'); return }
  if (!draft.value.models.length) { formError.value = t('groupQuality.invalidModels'); return }
  saving.value = true; formError.value = ''
  try {
    await saveQualityConfig({ ...draft.value, cooldown_seconds: cooldownMinutes.value * 60, auto_interval_seconds: autoMinutes.value * 60 })
    draft.value = null; app.showSuccess(t('groupQuality.saved')); await load()
  } catch (e) { formError.value = extractApiErrorMessage(e) }
  finally { saving.value = false }
}
async function confirmDelete() {
  if (!deleting.value) return
  const entry = deleting.value
  try {
    await deleteQualityConfig(entry.monitor_id)
    deleting.value = null
    app.showSuccess(t('groupQuality.deleted'))
    await load()
  } catch (e) { deleting.value = null; app.showError(extractApiErrorMessage(e)) }
}
onMounted(() => { void load(); void loadMonitors() })
</script>
