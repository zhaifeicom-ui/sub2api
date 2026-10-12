<template>
  <AppLayout>
    <div class="space-y-5">
      <header class="page-header">
        <div><h1 class="page-title">{{ t('groupQuality.title') }}</h1><p class="page-description">{{ t('groupQuality.description') }}</p></div>
        <button class="btn btn-secondary" :disabled="loading" @click="load">{{ t('groupQuality.refresh') }}</button>
      </header>
      <div class="card space-y-2 border-amber-200 bg-amber-50 text-sm text-amber-800 dark:border-amber-900 dark:bg-amber-950/20 dark:text-amber-200">
        <p>{{ t('groupQuality.warning') }}</p><p class="text-xs">{{ t('groupQuality.availabilityHint') }}</p>
      </div>
      <SearchInput v-model="search" :placeholder="t('groupQuality.search')" class="max-w-sm" />
      <div v-if="error" role="alert" class="rounded-xl bg-red-50 p-3 text-red-700 dark:bg-red-950/30 dark:text-red-300">{{ error }}</div>
      <div class="table-container">
        <table class="table">
          <thead><tr><th>{{ t('groupQuality.group') }}</th><th>{{ t('groupQuality.enabled') }}</th><th>{{ t('groupQuality.models') }}</th><th>{{ t('groupQuality.access') }}</th><th>{{ t('groupQuality.cooldown') }}</th><th></th></tr></thead>
          <tbody>
            <tr v-for="config in filtered" :key="config.group_id">
              <td><p class="font-medium">{{ config.group_name }}</p><p class="text-xs text-gray-400">{{ config.platform }} · #{{ config.group_id }}</p></td>
              <td><span class="badge" :class="config.enabled ? 'badge-success' : 'badge-gray'">{{ config.enabled ? t('common.enabled') : t('common.disabled') }}</span></td>
              <td><div v-for="model in config.models" :key="model" class="text-xs"><span class="font-mono">{{ model }}</span><span v-if="!config.available_models.includes(model)" class="ml-1 text-amber-600">{{ t('groupQuality.unavailable') }}</span></div></td>
              <td>{{ t(`groupQuality.${config.customer_access}`) }}</td>
              <td><p>{{ config.cooldown_seconds / 60 }}</p><p class="mt-1 text-xs text-gray-500">{{ config.auto_enabled ? t('groupQuality.autoEvery', { minutes: (config.auto_interval_seconds || 600) / 60 }) : t('groupQuality.autoOff') }}</p></td>
              <td><div class="flex justify-end gap-2"><button class="btn btn-secondary btn-sm" @click="edit(config)">{{ t('groupQuality.edit') }}</button><button class="btn btn-primary btn-sm" :disabled="!config.enabled" @click="target = config">{{ t('groupQuality.test') }}</button></div></td>
            </tr>
            <tr v-if="!filtered.length"><td colspan="6" class="py-10 text-center text-gray-400">{{ loading ? t('groupQuality.pending') : t('groupQuality.empty') }}</td></tr>
          </tbody>
        </table>
      </div>
    </div>
    <BaseDialog :show="!!draft" :title="`${t('groupQuality.edit')} · ${draft?.group_name || ''}`" @close="closeEdit">
      <form v-if="draft" id="quality-config-form" class="space-y-5" @submit.prevent="save">
        <div class="flex items-center justify-between"><label class="input-label">{{ t('groupQuality.enabled') }}</label><Toggle v-model="draft.enabled" :aria-label="t('groupQuality.enabled')" /></div>
        <fieldset><legend class="input-label">{{ t('groupQuality.models') }}</legend><label v-for="model in QUALITY_MODELS" :key="model" class="mr-4 inline-flex items-center gap-2 text-sm"><input v-model="draft.models" type="checkbox" :value="model" class="checkbox" />{{ model }}</label></fieldset>
        <div><label class="input-label">{{ t('groupQuality.access') }}</label><Select v-model="draft.customer_access" :options="accessOptions" /><p class="mt-2 text-xs text-gray-500">{{ t('groupQuality.customerHint') }}</p></div>
        <div><label class="input-label">{{ t('groupQuality.protocol') }}</label><Select v-model="draft.api_mode" :options="protocolOptions" /></div>
        <div><label class="input-label">{{ t('groupQuality.account') }}</label><Select v-model="accountValue" :options="accountOptions" :disabled="accountsLoading" /><p class="mt-2 text-xs text-gray-500">{{ t('groupQuality.direct') }}</p></div>
        <div><label for="quality-cooldown" class="input-label">{{ t('groupQuality.cooldown') }}</label><input id="quality-cooldown" v-model.number="cooldownMinutes" type="number" min="5" max="1440" step="1" class="input" required /></div>
        <div class="flex items-center justify-between"><label class="input-label">{{ t('groupQuality.autoDetection') }}</label><Toggle v-model="autoEnabled" :aria-label="t('groupQuality.autoDetection')" /></div>
        <div><label for="quality-auto-interval" class="input-label">{{ t('groupQuality.autoInterval') }}</label><div class="mb-2 flex flex-wrap gap-2"><button v-for="minutes in [10, 30, 60, 120, 360, 1440]" :key="minutes" type="button" class="btn btn-secondary btn-sm" @click="autoMinutes = minutes">{{ t('groupQuality.intervalMinutes', { minutes }) }}</button></div><input id="quality-auto-interval" v-model.number="autoMinutes" type="number" min="10" max="525600" step="1" class="input" required /><p class="mt-2 text-xs text-amber-600 dark:text-amber-300">{{ t('groupQuality.autoCostHint') }}</p></div>
        <div v-if="formError" role="alert" class="text-sm text-red-600">{{ formError }}</div>
      </form>
      <template #footer><button class="btn btn-secondary" :disabled="saving" @click="closeEdit">{{ t('groupQuality.cancel') }}</button><button type="submit" form="quality-config-form" class="btn btn-primary" :disabled="saving || accountsLoading">{{ t('groupQuality.save') }}</button></template>
    </BaseDialog>
    <GroupQualityDialog v-if="target" :show="true" :config="target" :admin="true" @close="target = null" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import GroupQualityDialog from '@/components/quality/GroupQualityDialog.vue'
import { QUALITY_MODELS, qualityConfigs, saveQualityConfig, type QualityConfig } from '@/api/groupQuality'
import { list as listAccounts } from '@/api/admin/accounts'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const app = useAppStore()
const configs = ref<QualityConfig[]>([])
const search = ref('')
const loading = ref(false)
const error = ref('')
const draft = ref<QualityConfig | null>(null)
const target = ref<QualityConfig | null>(null)
const saving = ref(false)
const formError = ref('')
const accountsLoading = ref(false)
const cooldownMinutes = ref(10)
const autoMinutes = ref(10)
const autoEnabled = computed({ get: () => draft.value?.auto_enabled ?? false, set: value => { if (draft.value) draft.value.auto_enabled = value } })
const accounts = ref<{ id: number; name: string }[]>([])
let editGeneration = 0
const filtered = computed(() => configs.value.filter(c => c.group_name.toLowerCase().includes(search.value.toLowerCase())))
const accessOptions = computed(() => ['hidden', 'view', 'run'].map(value => ({ value, label: t(`groupQuality.${value}`) })))
const protocolOptions = [{ value: 'responses', label: 'Responses' }, { value: 'chat_completions', label: 'Chat Completions' }]
const accountOptions = computed(() => [{ value: '', label: t('groupQuality.auto') }, ...accounts.value.map(a => ({ value: String(a.id), label: `${a.name} · #${a.id}` }))])
const accountValue = computed({ get: () => draft.value?.account_id ? String(draft.value.account_id) : '', set: value => { if (draft.value) draft.value.account_id = value ? Number(value) : null } })
async function load() {
  loading.value = true; error.value = ''
  try { configs.value = await qualityConfigs(true) } catch (e) { error.value = extractApiErrorMessage(e) }
  finally { loading.value = false }
}
function closeEdit() { if (!saving.value) { editGeneration++; draft.value = null } }
async function edit(config: QualityConfig) {
  const token = ++editGeneration
  draft.value = { ...config, models: [...config.models] }; cooldownMinutes.value = config.cooldown_seconds / 60; formError.value = ''; accounts.value = []; accountsLoading.value = true
  autoMinutes.value = (config.auto_interval_seconds || 600) / 60
  try {
    // Lite responses avoid fetching account credentials for this selector.
    for (let page = 1; page <= 10; page++) {
      const result = await listAccounts(page, 100, { platform: 'openai', type: 'apikey', group: String(config.group_id), lite: 'true', include_scheduler_score: 'false' })
      if (token !== editGeneration) return
      accounts.value.push(...result.items.map(a => ({ id: a.id, name: a.name })))
      if (accounts.value.length >= result.total || !result.items.length) break
    }
  } catch (e) { if (token === editGeneration) formError.value = extractApiErrorMessage(e) }
  finally { if (token === editGeneration) accountsLoading.value = false }
}
async function save() {
  if (!draft.value || saving.value) return
  if (!draft.value.models.length) { formError.value = t('groupQuality.invalidModels'); return }
  saving.value = true; formError.value = ''
  try {
    await saveQualityConfig({ ...draft.value, cooldown_seconds: cooldownMinutes.value * 60, auto_interval_seconds: autoMinutes.value * 60 })
    draft.value = null; app.showSuccess(t('groupQuality.saved')); await load()
  } catch (e) { formError.value = extractApiErrorMessage(e) }
  finally { saving.value = false }
}
onMounted(load)
</script>
