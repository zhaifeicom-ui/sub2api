<template>
  <AppLayout>
    <TablePageLayout>
      <template #actions>
        <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
          <div>
            <div class="flex flex-wrap items-center gap-3">
              <h1 class="text-2xl font-semibold tracking-tight text-gray-950 dark:text-white">{{ t('admin.promptInjection.title') }}</h1>
              <span class="rounded-full bg-primary-50 px-2.5 py-1 text-xs font-medium text-primary-700 dark:bg-primary-950/40 dark:text-primary-300">{{ t('admin.promptInjection.ruleCount', { count: rules.length }) }}</span>
            </div>
            <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-dark-300">{{ t('admin.promptInjection.description') }}</p>
          </div>
          <div class="flex shrink-0 items-center gap-3">
            <button type="button" class="btn btn-secondary" @click="openEvents">{{ t('admin.promptInjection.events') }}</button>
            <button type="button" class="btn btn-secondary" :disabled="loading || saving" :title="t('common.refresh')" @click="loadData"><Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" /></button>
            <button type="button" class="btn btn-primary" :disabled="saving" @click="openCreate"><Icon name="plus" size="md" class="mr-2" />{{ t('admin.promptInjection.addRule') }}</button>
          </div>
        </div>
      </template>

      <template #filters>
        <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div class="relative w-full sm:w-80">
            <Icon name="search" size="md" class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
            <input v-model="searchText" class="input pl-10" :placeholder="t('admin.promptInjection.searchPlaceholder')" />
          </div>
          <div class="flex items-center gap-2 text-xs text-gray-500 dark:text-dark-300"><span class="inline-flex h-2 w-2 rounded-full bg-emerald-500"></span>{{ t('admin.promptInjection.positionSummary') }}</div>
        </div>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="filteredRules" :loading="loading" :row-key="'id'" :actions-count="3">
          <template #cell-action="{ row }"><span class="badge badge-gray">{{ t(`admin.promptInjection.policyActions.${row.action || 'append'}`) }}</span></template>
          <template #empty>
            <div class="flex flex-col items-center py-8 text-center">
              <Icon name="shield" size="xl" class="mb-4 h-12 w-12 text-gray-300 dark:text-dark-500" />
              <p class="text-lg font-medium text-gray-900 dark:text-gray-100">{{ t('admin.promptInjection.empty') }}</p>
              <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.promptInjection.emptyHint') }}</p>
              <button type="button" class="btn btn-primary mt-5" @click="openCreate"><Icon name="plus" size="md" class="mr-2" />{{ t('admin.promptInjection.addRule') }}</button>
            </div>
          </template>
          <template #cell-name="{ row }"><div class="min-w-44"><div class="font-medium text-gray-900 dark:text-white">{{ row.name }}</div><div class="mt-1 font-mono text-[11px] text-gray-400">{{ row.id }}</div></div></template>
          <template #cell-groups="{ row }">
            <div class="flex max-w-xs flex-wrap gap-1.5">
              <span v-for="group in groupsForRule(row)" :key="group.id" class="inline-flex items-center rounded-full bg-gray-100 px-2.5 py-1 text-xs font-medium text-gray-700 dark:bg-dark-700 dark:text-dark-100">#{{ group.id }} {{ group.name }}</span>
              <span v-if="groupsForRule(row).length === 0" class="text-xs text-amber-600 dark:text-amber-300">{{ t('admin.promptInjection.missingGroup') }}</span>
            </div>
          </template>
          <template #cell-role="{ value, row }"><span v-if="!row.action || row.action === 'append' || row.action === 'replace_system'" class="rounded-md bg-primary-50 px-2 py-1 text-xs font-semibold text-primary-700 dark:bg-primary-950/40 dark:text-primary-300">{{ t(`admin.promptInjection.roles.${row.action === 'replace_system' ? 'system' : value}`) }}</span><span v-else class="text-gray-400">—</span></template>
          <template #cell-position="{ value, row }"><div v-if="!row.action || row.action === 'append'" class="min-w-44"><div class="text-sm font-medium text-gray-800 dark:text-dark-100">{{ t(`admin.promptInjection.positions.${value}.title`) }}</div><div class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">{{ t(`admin.promptInjection.positions.${value}.short`) }}</div></div><span v-else class="text-gray-400">—</span></template>
          <template #cell-content="{ value }"><div class="max-w-sm truncate text-sm text-gray-600 dark:text-dark-200" :title="value">{{ value }}</div></template>
          <template #cell-actions="{ row }"><div class="flex flex-wrap justify-end gap-2"><button type="button" class="btn btn-secondary btn-sm" :disabled="saving" @click="openEdit(row)">{{ t('admin.promptInjection.actions.edit') }}</button><button type="button" class="btn btn-secondary btn-sm" :disabled="saving" @click="duplicateRule(row)">{{ t('admin.promptInjection.actions.duplicate') }}</button><button type="button" class="btn btn-danger btn-sm" :disabled="saving" @click="deleteRule(row)">{{ t('admin.promptInjection.actions.delete') }}</button></div></template>
        </DataTable>
      </template>
    </TablePageLayout>

    <BaseDialog :show="dialogOpen" :title="editingID ? t('admin.promptInjection.editRule') : t('admin.promptInjection.addRule')" width="wide" @close="dialogOpen = false">
      <form class="space-y-6" @submit.prevent="confirmDraft">
        <div class="grid gap-5 md:grid-cols-2">
          <label class="block md:col-span-2"><span class="input-label">{{ t('admin.promptInjection.fields.name') }}</span><input v-model="draft.name" class="input mt-1" :placeholder="t('admin.promptInjection.placeholders.name')" /></label>
          <div class="md:col-span-2"><GroupSelector v-model="draft.group_ids" :groups="groups" :searchable="'auto'" /><p class="input-hint mt-1">{{ t('admin.promptInjection.hints.groups') }}</p></div>
          <div class="md:col-span-2"><span class="input-label">{{ t('admin.promptInjection.policyAction') }}</span><Select v-model="draft.action" :options="actionOptions" class="mt-1" /><p class="input-hint mt-1">{{ t('admin.promptInjection.replacementHint') }}</p></div>
          <label v-if="draft.action === 'replace_input'" class="flex items-center gap-2 md:col-span-2"><input v-model="draft.whole_input" type="checkbox" class="checkbox" />{{ t('admin.promptInjection.wholeInput') }}</label>
          <label v-if="draft.action === 'replace_output' || (draft.action === 'replace_input' && !draft.whole_input)" class="block md:col-span-2"><span class="input-label">{{ t('admin.promptInjection.matchText') }}</span><textarea v-model="draft.match" class="input mt-1" rows="2"></textarea></label>

          <div v-if="!draft.action || draft.action === 'append'" class="md:col-span-2">
            <span class="input-label">{{ t('admin.promptInjection.fields.role') }}</span>
            <div class="mt-2 grid gap-2 sm:grid-cols-3">
              <button v-for="role in roleOptions" :key="role.value" type="button" class="rounded-xl border p-3 text-left transition-colors" :class="draft.role === role.value ? 'border-primary-500 bg-primary-50 dark:border-primary-500 dark:bg-primary-950/30' : 'border-gray-200 bg-white hover:border-primary-300 dark:border-dark-600 dark:bg-dark-800'" @click="draft.role = role.value"><div class="text-sm font-semibold text-gray-900 dark:text-white">{{ t(`admin.promptInjection.roles.${role.value}`) }}</div><div class="mt-1 text-xs text-gray-500 dark:text-dark-300">{{ t(`admin.promptInjection.roleHints.${role.value}`) }}</div></button>
            </div>
          </div>

          <div v-if="!draft.action || draft.action === 'append'" class="md:col-span-2">
            <div class="flex items-center justify-between gap-3"><span class="input-label">{{ t('admin.promptInjection.fields.position') }}</span><span class="text-xs text-primary-600 dark:text-primary-300">{{ t('admin.promptInjection.positionTechnicalHint') }}</span></div>
            <div class="mt-2 grid gap-2 md:grid-cols-3">
              <button v-for="position in positionOptions" :key="position.value" type="button" class="rounded-xl border p-3 text-left transition-colors" :class="draft.position === position.value ? 'border-primary-500 bg-primary-50 dark:border-primary-500 dark:bg-primary-950/30' : 'border-gray-200 bg-white hover:border-primary-300 dark:border-dark-600 dark:bg-dark-800'" @click="draft.position = position.value"><div class="text-sm font-semibold text-gray-900 dark:text-white">{{ t(`admin.promptInjection.positions.${position.value}.title`) }}</div><div class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-300">{{ t(`admin.promptInjection.positions.${position.value}.description`) }}</div><span v-if="position.recommended" class="mt-2 inline-flex rounded-full bg-emerald-100 px-2 py-0.5 text-[11px] font-medium text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300">{{ t('admin.promptInjection.recommended') }}</span></button>
            </div>
          </div>

          <div v-if="!draft.action || draft.action === 'append'" class="rounded-xl border border-primary-100 bg-primary-50/70 p-4 dark:border-primary-900/50 dark:bg-primary-950/20 md:col-span-2"><div class="flex gap-3"><Icon name="infoCircle" size="md" class="mt-0.5 shrink-0 text-primary-600 dark:text-primary-300" /><div><p class="text-sm font-medium text-primary-900 dark:text-primary-100">{{ t('admin.promptInjection.positionExplainTitle') }}</p><p class="mt-1 text-xs leading-5 text-primary-800/80 dark:text-primary-200/80">{{ positionExplanation }}</p></div></div></div>
          <label class="block md:col-span-2"><span class="input-label">{{ draft.action === 'replace_input' || draft.action === 'replace_output' ? t('admin.promptInjection.replacementText') : t('admin.promptInjection.fields.content') }}</span><textarea v-model="draft.content" rows="9" class="input mt-1 font-mono" :placeholder="t('admin.promptInjection.placeholders.content')"></textarea><p class="input-hint mt-1">{{ t('admin.promptInjection.hints.content') }}</p></label>
          <div v-if="draft.action === 'replace_input' || draft.action === 'replace_output'" class="md:col-span-2 space-y-2"><label class="input-label">{{ t('admin.promptInjection.previewText') }}</label><textarea v-model="previewInput" class="input" rows="3"></textarea><pre class="whitespace-pre-wrap rounded-xl bg-gray-50 p-3 text-sm dark:bg-dark-700">{{ replacementPreview }}</pre></div>
        </div>
        <div class="flex justify-end gap-3 border-t border-gray-200 pt-4 dark:border-dark-700"><button type="button" class="btn btn-secondary" :disabled="saving" @click="dialogOpen = false">{{ t('admin.promptInjection.actions.cancel') }}</button><button type="submit" class="btn btn-primary" :disabled="saving">{{ saving ? t('common.saving') : t('admin.promptInjection.actions.confirm') }}</button></div>
      </form>
    </BaseDialog>
    <BaseDialog :show="eventsOpen" :title="t('admin.promptInjection.events')" width="extra-wide" @close="eventsOpen = false"><p class="mb-3 text-xs text-gray-500">{{ t('admin.promptInjection.eventsHint') }}</p><button class="btn btn-secondary btn-sm mb-3" :disabled="eventsLoading" @click="openEvents">{{ t('common.refresh') }}</button><div class="table-container"><table class="table"><thead><tr><th>Request ID</th><th>{{ t('admin.promptInjection.columns.groups') }}</th><th>{{ t('admin.promptInjection.eventStatus') }}</th><th>{{ t('admin.promptInjection.eventBytes') }}</th></tr></thead><tbody><tr v-for="event in events" :key="event.request_id + event.created_at"><td class="text-xs"><p>{{ event.request_id }}</p><p>{{ new Date(event.created_at).toLocaleString() }}</p></td><td>#{{ event.group_id }} · v{{ event.version }}</td><td>{{ event.status }}<p v-if="event.transport_status" class="text-xs">Transport: {{ event.transport_status }}</p><p class="text-xs">{{ event.protocol }} · {{ event.rule_ids.length }} rules</p></td><td>{{ event.input_bytes_before }} → {{ event.input_bytes_after }}</td></tr></tbody></table></div></BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import GroupSelector from '@/components/common/GroupSelector.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import * as groupsAPI from '@/api/admin/groups'
import { getPromptInjections, updatePromptInjections, getManagedPolicyEvents, type ManagedPolicyEvent, type PromptInjectionRule } from '@/api/admin/promptInjections'
import type { AdminGroup } from '@/types'

const { t } = useI18n()
const appStore = useAppStore()
const rules = ref<PromptInjectionRule[]>([])
const configVersion = ref(0)
const eventsOpen = ref(false)
const eventsLoading = ref(false)
const events = ref<ManagedPolicyEvent[]>([])
const previewInput = ref('')
async function openEvents() { eventsOpen.value = true; eventsLoading.value = true; try { events.value = await getManagedPolicyEvents() } catch(e) { appStore.showError(extractApiErrorMessage(e)) } finally { eventsLoading.value = false } }
const groups = ref<AdminGroup[]>([])
const loading = ref(true)
const saving = ref(false)
const searchText = ref('')
const dialogOpen = ref(false)
const editingID = ref<string | null>(null)

const roleOptions = [{ value: 'system' as const }, { value: 'developer' as const }, { value: 'user' as const }]
const positionOptions = [{ value: 'prepend' as const, recommended: false }, { value: 'before_first_user' as const, recommended: true }, { value: 'after_last_user' as const, recommended: false }]
const blankRule = (): PromptInjectionRule => ({ id: crypto.randomUUID(), name: '', enabled: true, scope: 'group', target_id: 0, group_ids: [], role: 'developer', position: 'before_first_user', content: '', models: [], priority: 100, action: 'append', match: '' })
const draft = ref<PromptInjectionRule>(blankRule())
const replacementPreview = computed(() => draft.value.action === 'replace_input' && draft.value.whole_input ? draft.value.content : draft.value.match ? previewInput.value.split(draft.value.match).join(draft.value.content) : previewInput.value)

const columns = computed(() => [
  { key: 'name', label: t('admin.promptInjection.columns.name'), sortable: false },
  { key: 'action', label: t('admin.promptInjection.policyAction'), sortable: false },
  { key: 'groups', label: t('admin.promptInjection.columns.groups'), sortable: false },
  { key: 'role', label: t('admin.promptInjection.columns.role'), sortable: false },
  { key: 'position', label: t('admin.promptInjection.columns.position'), sortable: false },
  { key: 'content', label: t('admin.promptInjection.columns.content'), sortable: false },
  { key: 'actions', label: t('admin.promptInjection.columns.actions'), sortable: false }
])
const filteredRules = computed(() => {
  const query = searchText.value.trim().toLowerCase()
  if (!query) return rules.value
  return rules.value.filter((rule) => `${rule.name} ${rule.content} ${groupsForRule(rule).map((group) => `${group.id} ${group.name}`).join(' ')}`.toLowerCase().includes(query))
})
const positionExplanation = computed(() => t(`admin.promptInjection.positions.${draft.value.position}.explain`))
const actionOptions = computed(() => ['append', 'replace_system', 'replace_input', 'replace_output'].map(value => ({ value, label: t(`admin.promptInjection.policyActions.${value}`) })))

function groupsForRule(rule: PromptInjectionRule) {
  const ids = rule.group_ids?.length ? rule.group_ids : rule.target_id ? [rule.target_id] : []
  return ids.map((id) => groups.value.find((group) => group.id === id)).filter((group): group is AdminGroup => !!group)
}
function normalizeRule(rule: PromptInjectionRule): PromptInjectionRule {
  const groupIds = [...new Set(rule.group_ids?.length ? rule.group_ids : rule.target_id ? [rule.target_id] : [])]
  return { ...rule, action: rule.action || 'append', scope: 'group', enabled: rule.enabled, group_ids: groupIds, target_id: groupIds[0] || 0, models: [], priority: 100 }
}
function openCreate() { editingID.value = null; draft.value = blankRule(); dialogOpen.value = true }
function openEdit(rule: PromptInjectionRule) { editingID.value = rule.id; draft.value = { ...normalizeRule(rule), group_ids: [...(rule.group_ids?.length ? rule.group_ids : rule.target_id ? [rule.target_id] : [])] }; dialogOpen.value = true }
async function persistRules(nextRules: PromptInjectionRule[], successMessage: string) {
  saving.value = true
  try { const result = await updatePromptInjections({ version: configVersion.value, rules: nextRules.map(normalizeRule) }); configVersion.value = result.version || 0; rules.value = (result.rules || []).map(normalizeRule); appStore.showSuccess(successMessage); return true }
  catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.promptInjection.saveFailed'))); return false }
  finally { saving.value = false }
}
async function confirmDraft() {
  if (!draft.value.name.trim()) return appStore.showError(t('admin.promptInjection.validation.name'))
  if (!draft.value.group_ids?.length) return appStore.showError(t('admin.promptInjection.validation.groups'))
  if ((draft.value.action === 'append' || draft.value.action === 'replace_system') && !draft.value.content.trim()) return appStore.showError(t('admin.promptInjection.validation.content'))
  if ((draft.value.action === 'replace_output' || (draft.value.action === 'replace_input' && !draft.value.whole_input)) && !draft.value.match) return appStore.showError(t('admin.promptInjection.matchRequired'))
  const next = normalizeRule({ ...draft.value, name: draft.value.name.trim(), content: draft.value.action === 'replace_input' || draft.value.action === 'replace_output' ? draft.value.content : draft.value.content.trim() })
  const nextRules = rules.value.slice(); const index = nextRules.findIndex((rule) => rule.id === editingID.value)
  if (index >= 0) nextRules[index] = next; else nextRules.push(next)
  if (await persistRules(nextRules, t('admin.promptInjection.saved'))) dialogOpen.value = false
}
async function duplicateRule(rule: PromptInjectionRule) { const duplicate = { ...normalizeRule(rule), id: crypto.randomUUID(), name: `${rule.name} - ${t('admin.promptInjection.copySuffix')}`, group_ids: [...(rule.group_ids || [])] }; await persistRules([...rules.value, duplicate], t('admin.promptInjection.saved')) }
async function deleteRule(rule: PromptInjectionRule) { if (!window.confirm(t('admin.promptInjection.confirmDelete', { name: rule.name }))) return; await persistRules(rules.value.filter((item) => item.id !== rule.id), t('admin.promptInjection.deleted')) }
async function loadData() { loading.value = true; try { const [config, groupList] = await Promise.all([getPromptInjections(), groupsAPI.getAllIncludingInactive()]); configVersion.value = config.version || 0; rules.value = (config.rules || []).map(normalizeRule); groups.value = groupList } catch (error) { appStore.showError(extractApiErrorMessage(error, t('admin.promptInjection.loadFailed'))) } finally { loading.value = false } }
onMounted(loadData)
</script>
