<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl pb-28">
      <header class="mb-6 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 class="text-2xl font-semibold tracking-tight text-gray-950 dark:text-white">{{ t('admin.promptInjection.title') }}</h1>
          <p class="mt-2 max-w-3xl text-sm text-gray-500 dark:text-dark-300">{{ t('admin.promptInjection.description') }}</p>
        </div>
        <button type="button" class="btn btn-primary" @click="openCreate">
          <Icon name="plus" size="md" class="mr-2" />{{ t('admin.promptInjection.addRule') }}
        </button>
      </header>

      <div class="mb-5 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:border-amber-900/70 dark:bg-amber-950/30 dark:text-amber-200">
        {{ t('admin.promptInjection.warning') }}
      </div>

      <div v-if="loading" class="card p-10 text-center text-gray-500">{{ t('common.loading') }}</div>
      <div v-else-if="rules.length === 0" class="card p-12 text-center">
        <p class="text-sm text-gray-500 dark:text-dark-300">{{ t('admin.promptInjection.empty') }}</p>
      </div>
      <div v-else class="space-y-4">
        <article v-for="rule in rules" :key="rule.id" class="card p-5">
          <div class="flex flex-wrap items-start justify-between gap-4">
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-2">
                <h2 class="font-semibold text-gray-950 dark:text-white">{{ rule.name }}</h2>
                <span :class="rule.enabled ? 'bg-green-100 text-green-700 dark:bg-green-950/50 dark:text-green-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-300'" class="rounded-full px-2.5 py-0.5 text-xs font-medium">
                  {{ rule.enabled ? t('common.enabled') : t('common.disabled') }}
                </span>
                <span class="rounded-full bg-primary-50 px-2.5 py-0.5 text-xs font-medium text-primary-700 dark:bg-primary-950/40 dark:text-primary-300">
                  {{ t(`admin.promptInjection.scopes.${rule.scope}`) }} · {{ targetLabel(rule) }}
                </span>
              </div>
              <div class="mt-3 flex flex-wrap gap-x-5 gap-y-2 text-xs text-gray-500 dark:text-dark-300">
                <span>{{ t('admin.promptInjection.fields.role') }}: {{ t(`admin.promptInjection.roles.${rule.role}`) }}</span>
                <span>{{ t('admin.promptInjection.fields.position') }}: {{ t(`admin.promptInjection.positions.${rule.position}`) }}</span>
                <span>{{ t('admin.promptInjection.fields.priority') }}: {{ rule.priority }}</span>
                <span>{{ t('admin.promptInjection.fields.models') }}: {{ rule.models.length ? rule.models.join(', ') : t('admin.promptInjection.allModels') }}</span>
              </div>
              <pre class="mt-4 max-h-40 overflow-auto whitespace-pre-wrap rounded-lg bg-gray-50 p-3 text-sm text-gray-700 dark:bg-dark-800 dark:text-dark-200">{{ rule.content }}</pre>
            </div>
            <div class="flex shrink-0 items-center gap-2">
              <button type="button" class="btn btn-secondary btn-sm" @click="toggleRule(rule)">{{ rule.enabled ? t('common.disabled') : t('common.enabled') }}</button>
              <button type="button" class="btn btn-secondary btn-sm" @click="openEdit(rule)">{{ t('admin.promptInjection.actions.edit') }}</button>
              <button type="button" class="btn btn-secondary btn-sm" @click="duplicateRule(rule)">{{ t('admin.promptInjection.actions.duplicate') }}</button>
              <button type="button" class="btn btn-danger btn-sm" @click="deleteRule(rule.id)">{{ t('admin.promptInjection.actions.delete') }}</button>
            </div>
          </div>
        </article>
      </div>
    </div>

    <div class="fixed inset-x-0 bottom-0 z-30 border-t border-gray-200 bg-white/95 px-4 py-3 shadow-[0_-12px_35px_rgba(15,23,42,0.08)] backdrop-blur dark:border-dark-700 dark:bg-dark-900/95 lg:left-64">
      <div class="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-3">
        <div>
          <p class="text-sm font-medium text-gray-800 dark:text-dark-100">{{ t('admin.promptInjection.enabledCount', { enabled: enabledCount, total: rules.length }) }}</p>
          <p class="text-xs" :class="dirty ? 'text-amber-700 dark:text-amber-300' : 'text-gray-500 dark:text-dark-400'">{{ dirty ? t('admin.promptInjection.unsaved') : t('admin.promptInjection.synced') }}</p>
        </div>
        <div class="flex gap-3">
          <button type="button" class="btn btn-secondary" :disabled="!dirty || saving" @click="resetRules">{{ t('common.reset') }}</button>
          <button type="button" class="btn btn-primary" :disabled="!dirty || saving" @click="saveRules">{{ saving ? t('common.saving') : t('admin.promptInjection.saveAll') }}</button>
        </div>
      </div>
    </div>

    <BaseDialog :show="dialogOpen" :title="editingID ? t('admin.promptInjection.editRule') : t('admin.promptInjection.addRule')" width="wide" @close="dialogOpen = false">
      <form class="grid gap-5 md:grid-cols-2" @submit.prevent="confirmDraft">
        <label class="block"><span class="label">{{ t('admin.promptInjection.fields.name') }}</span><input v-model="draft.name" class="input mt-1" :placeholder="t('admin.promptInjection.placeholders.name')" /></label>
        <label class="flex items-center gap-3 pt-7"><input v-model="draft.enabled" type="checkbox" class="h-4 w-4 rounded" /><span class="text-sm">{{ t('admin.promptInjection.fields.enabled') }}</span></label>
        <label class="block"><span class="label">{{ t('admin.promptInjection.fields.scope') }}</span><select v-model="draft.scope" class="input mt-1" @change="draft.target_id = 0"><option value="group">{{ t('admin.promptInjection.scopes.group') }}</option><option value="account">{{ t('admin.promptInjection.scopes.account') }}</option></select></label>
        <label class="block"><span class="label">{{ t('admin.promptInjection.fields.target') }}</span><select v-model.number="draft.target_id" class="input mt-1"><option :value="0">{{ t('admin.promptInjection.placeholders.target') }}</option><option v-for="item in targetOptions" :key="item.id" :value="item.id">#{{ item.id }} {{ item.name }}</option></select></label>
        <label class="block"><span class="label">{{ t('admin.promptInjection.fields.role') }}</span><select v-model="draft.role" class="input mt-1"><option value="system">System</option><option value="developer">Developer</option><option value="user">User</option></select><span class="mt-1 block text-xs text-gray-500">{{ t('admin.promptInjection.hints.role') }}</span></label>
        <label class="block"><span class="label">{{ t('admin.promptInjection.fields.position') }}</span><select v-model="draft.position" class="input mt-1"><option value="prepend">{{ t('admin.promptInjection.positions.prepend') }}</option><option value="before_first_user">{{ t('admin.promptInjection.positions.before_first_user') }}</option><option value="after_last_user">{{ t('admin.promptInjection.positions.after_last_user') }}</option></select></label>
        <label class="block md:col-span-2"><span class="label">{{ t('admin.promptInjection.fields.models') }}</span><input v-model="modelsText" class="input mt-1" :placeholder="t('admin.promptInjection.placeholders.models')" /><span class="mt-1 block text-xs text-gray-500">{{ t('admin.promptInjection.hints.models') }}</span></label>
        <label class="block"><span class="label">{{ t('admin.promptInjection.fields.priority') }}</span><input v-model.number="draft.priority" type="number" class="input mt-1" /><span class="mt-1 block text-xs text-gray-500">{{ t('admin.promptInjection.hints.priority') }}</span></label>
        <label class="block md:col-span-2"><span class="label">{{ t('admin.promptInjection.fields.content') }}</span><textarea v-model="draft.content" rows="10" class="input mt-1 font-mono" :placeholder="t('admin.promptInjection.placeholders.content')"></textarea></label>
        <div class="md:col-span-2 flex justify-end gap-3 border-t border-gray-200 pt-4 dark:border-dark-700">
          <button type="button" class="btn btn-secondary" @click="dialogOpen = false">{{ t('admin.promptInjection.actions.cancel') }}</button>
          <button type="submit" class="btn btn-primary">{{ t('admin.promptInjection.actions.confirm') }}</button>
        </div>
      </form>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import * as groupsAPI from '@/api/admin/groups'
import * as accountsAPI from '@/api/admin/accounts'
import { getPromptInjections, updatePromptInjections, type PromptInjectionRule } from '@/api/admin/promptInjections'

const { t } = useI18n()
const appStore = useAppStore()
const rules = ref<PromptInjectionRule[]>([])
const savedRules = ref<PromptInjectionRule[]>([])
const groups = ref<{ id: number; name: string }[]>([])
const accounts = ref<{ id: number; name: string }[]>([])
const loading = ref(true)
const saving = ref(false)
const dialogOpen = ref(false)
const editingID = ref<string | null>(null)
const modelsText = ref('')

const blankRule = (): PromptInjectionRule => ({ id: crypto.randomUUID(), name: '', enabled: true, scope: 'group', target_id: 0, role: 'developer', position: 'prepend', content: '', models: [], priority: 100 })
const draft = ref<PromptInjectionRule>(blankRule())
const clone = (value: PromptInjectionRule[]) => JSON.parse(JSON.stringify(value)) as PromptInjectionRule[]
const fingerprint = (value: PromptInjectionRule[]) => JSON.stringify(value)
const dirty = computed(() => fingerprint(rules.value) !== fingerprint(savedRules.value))
const enabledCount = computed(() => rules.value.filter(rule => rule.enabled).length)
const targetOptions = computed(() => draft.value.scope === 'group' ? groups.value : accounts.value)

function targetLabel(rule: PromptInjectionRule) {
  const item = (rule.scope === 'group' ? groups.value : accounts.value).find(entry => entry.id === rule.target_id)
  return item ? `#${item.id} ${item.name}` : `#${rule.target_id}`
}

function openCreate() { editingID.value = null; draft.value = blankRule(); modelsText.value = ''; dialogOpen.value = true }
function openEdit(rule: PromptInjectionRule) { editingID.value = rule.id; draft.value = { ...rule, models: [...rule.models] }; modelsText.value = rule.models.join(', '); dialogOpen.value = true }
function toggleRule(rule: PromptInjectionRule) { rule.enabled = !rule.enabled }
function duplicateRule(rule: PromptInjectionRule) { rules.value.push({ ...rule, id: crypto.randomUUID(), name: `${rule.name} - Copy`, models: [...rule.models] }) }
function deleteRule(id: string) { rules.value = rules.value.filter(rule => rule.id !== id) }

function confirmDraft() {
  if (!draft.value.name.trim()) return appStore.showError(t('admin.promptInjection.validation.name'))
  if (!draft.value.target_id) return appStore.showError(t('admin.promptInjection.validation.target'))
  if (!draft.value.content.trim()) return appStore.showError(t('admin.promptInjection.validation.content'))
  const next = { ...draft.value, name: draft.value.name.trim(), content: draft.value.content.trim(), models: modelsText.value.split(',').map(item => item.trim()).filter(Boolean) }
  const index = rules.value.findIndex(rule => rule.id === editingID.value)
  if (index >= 0) rules.value[index] = next
  else rules.value.push(next)
  dialogOpen.value = false
}

function resetRules() { rules.value = clone(savedRules.value) }
async function saveRules() {
  saving.value = true
  try {
    const result = await updatePromptInjections({ rules: rules.value })
    rules.value = clone(result.rules || [])
    savedRules.value = clone(rules.value)
    appStore.showSuccess(t('admin.promptInjection.saved'))
  } catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.promptInjection.saveFailed'))) }
  finally { saving.value = false }
}

onMounted(async () => {
  try {
    const [config, groupList, accountPage] = await Promise.all([
      getPromptInjections(), groupsAPI.getAllIncludingInactive(), accountsAPI.list(1, 1000)
    ])
    rules.value = clone(config.rules || [])
    savedRules.value = clone(rules.value)
    groups.value = groupList.map(item => ({ id: item.id, name: item.name }))
    accounts.value = accountPage.items.map(item => ({ id: item.id, name: item.name }))
  } catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.promptInjection.loadFailed'))) }
  finally { loading.value = false }
})
</script>
