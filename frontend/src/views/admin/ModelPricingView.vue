<template>
  <AppLayout>
    <TablePageLayout>
      <template #actions>
        <div class="space-y-4">
          <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
            <div class="pricing-stat-card">
              <span class="pricing-stat-label">{{ t('admin.modelPricing.stats.rules') }}</span>
              <strong class="pricing-stat-value">{{ allRows.length }}</strong>
              <span class="pricing-stat-hint">{{ t('admin.modelPricing.stats.rulesHint') }}</span>
            </div>
            <div class="pricing-stat-card">
              <span class="pricing-stat-label">{{ t('admin.modelPricing.stats.models') }}</span>
              <strong class="pricing-stat-value">{{ uniqueModelCount }}</strong>
              <span class="pricing-stat-hint">{{ t('admin.modelPricing.stats.modelsHint') }}</span>
            </div>
            <div class="pricing-stat-card">
              <span class="pricing-stat-label">{{ t('admin.modelPricing.stats.owners') }}</span>
              <strong class="pricing-stat-value">{{ configuredOwnerCount }}</strong>
              <span class="pricing-stat-hint">{{ t('admin.modelPricing.stats.ownersHint') }}</span>
            </div>
            <div class="pricing-stat-card">
              <span class="pricing-stat-label">{{ t('admin.modelPricing.stats.wildcards') }}</span>
              <strong class="pricing-stat-value">{{ wildcardRuleCount }}</strong>
              <span class="pricing-stat-hint">{{ t('admin.modelPricing.stats.wildcardsHint') }}</span>
            </div>
          </div>

          <div class="flex items-start gap-3 rounded-xl border border-blue-200 bg-blue-50 px-4 py-3 text-sm dark:border-blue-900/60 dark:bg-blue-950/25">
            <Icon name="infoCircle" size="md" class="mt-0.5 flex-shrink-0 text-blue-600 dark:text-blue-400" />
            <div class="min-w-0">
              <p class="font-medium text-blue-900 dark:text-blue-100">
                {{ t('admin.modelPricing.priorityTitle') }}
              </p>
              <p class="mt-0.5 text-blue-700 dark:text-blue-300">
                {{ t('admin.modelPricing.priorityHint') }}
              </p>
            </div>
          </div>
        </div>
      </template>

      <template #filters>
        <div class="rounded-2xl border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-800">
          <div class="flex flex-col gap-4">
            <div class="flex flex-col justify-between gap-3 lg:flex-row lg:items-center">
              <div class="inline-flex w-fit rounded-xl bg-gray-100 p-1 dark:bg-dark-700">
                <button
                  v-for="tab in scopeTabs"
                  :key="tab.value"
                  type="button"
                  class="rounded-lg px-4 py-2 text-sm font-medium transition-colors"
                  :class="activeScope === tab.value
                    ? 'bg-white text-primary-600 shadow-sm dark:bg-dark-800 dark:text-primary-400'
                    : 'text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'"
                  @click="setActiveScope(tab.value)"
                >
                  {{ tab.label }}
                  <span class="ml-1 text-xs opacity-70">{{ tab.count }}</span>
                </button>
              </div>

              <div class="flex items-center gap-2">
                <button type="button" class="btn btn-secondary" :disabled="loading" @click="loadData">
                  <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
                </button>
                <button type="button" class="btn btn-primary" @click="openCreateEditor">
                  <Icon name="plus" size="md" class="mr-2" />
                  {{ t('admin.modelPricing.addRule') }}
                </button>
              </div>
            </div>

            <div class="grid grid-cols-1 gap-3 md:grid-cols-[minmax(15rem,1fr)_12rem_14rem]">
              <div class="relative">
                <Icon name="search" size="md" class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
                <input
                  v-model.trim="searchQuery"
                  type="search"
                  class="input pl-10"
                  :placeholder="t('admin.modelPricing.searchPlaceholder')"
                />
              </div>
              <Select v-model="platformFilter" :options="platformFilterOptions" />
              <Select v-model="ownerFilter" :options="ownerFilterOptions" searchable />
            </div>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="pagedRows" :loading="loading" row-key="id">
          <template #cell-scope="{ row }">
            <span
              class="inline-flex rounded-full px-2.5 py-1 text-xs font-medium"
              :class="row.scope === 'group'
                ? 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-300'
                : 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'"
            >
              {{ row.scope === 'group' ? t('admin.modelPricing.scope.group') : t('admin.modelPricing.scope.channel') }}
            </span>
          </template>

          <template #cell-owner="{ row }">
            <div class="min-w-[9rem]">
              <div class="font-medium text-gray-900 dark:text-white">{{ row.ownerName }}</div>
              <div class="mt-1 flex items-center gap-1.5 text-xs text-gray-500 dark:text-gray-400">
                <span
                  class="h-1.5 w-1.5 rounded-full"
                  :class="row.ownerActive ? 'bg-emerald-500' : 'bg-gray-400'"
                ></span>
                {{ row.ownerActive ? t('common.active') : t('common.inactive') }}
              </div>
            </div>
          </template>

          <template #cell-platform="{ row }">
            <div class="flex items-center gap-2">
              <PlatformIcon :platform="row.platform" size="xs" />
              <span class="capitalize">{{ platformLabel(row.platform) }}</span>
            </div>
          </template>

          <template #cell-models="{ row }">
            <div class="flex min-w-[14rem] max-w-md flex-wrap gap-1.5">
              <span
                v-for="model in row.models.slice(0, 5)"
                :key="model"
                class="rounded-md bg-gray-100 px-2 py-1 font-mono text-xs text-gray-700 dark:bg-dark-700 dark:text-gray-200"
              >
                {{ model }}
              </span>
              <span v-if="row.models.length > 5" class="px-1 py-1 text-xs text-gray-400">
                +{{ row.models.length - 5 }}
              </span>
            </div>
          </template>

          <template #cell-mode="{ row }">
            <span class="whitespace-nowrap rounded-md bg-gray-100 px-2 py-1 text-xs font-medium text-gray-700 dark:bg-dark-700 dark:text-gray-300">
              {{ billingModeLabel(row.pricing.billing_mode) }}
            </span>
          </template>

          <template #cell-prices="{ row }">
            <div v-if="row.pricing.billing_mode === 'token'" class="min-w-[15rem] space-y-1 text-xs">
              <div class="grid grid-cols-2 gap-x-4">
                <span class="text-gray-500">{{ t('admin.modelPricing.input') }}</span>
                <span class="font-mono text-gray-900 dark:text-gray-100">{{ formatTokenPrice(row.pricing.input_price) }}</span>
                <span class="text-gray-500">{{ t('admin.modelPricing.output') }}</span>
                <span class="font-mono text-gray-900 dark:text-gray-100">{{ formatTokenPrice(row.pricing.output_price) }}</span>
                <span class="text-gray-500">{{ t('admin.modelPricing.cacheRead') }}</span>
                <span class="font-mono text-gray-900 dark:text-gray-100">{{ formatTokenPrice(row.pricing.cache_read_price) }}</span>
              </div>
              <div class="text-[11px] text-gray-400">$/MTok · “—” {{ t('admin.modelPricing.inheritHint') }}</div>
            </div>
            <div v-else class="min-w-[10rem]">
              <span class="font-mono font-medium text-gray-900 dark:text-white">
                {{ formatPerRequestPrice(row.pricing.per_request_price) }}
              </span>
              <span class="ml-1 text-xs text-gray-400">/{{ t('admin.modelPricing.request') }}</span>
            </div>
          </template>

          <template #cell-advanced="{ row }">
            <div class="flex min-w-[8rem] flex-wrap gap-1.5 text-xs">
              <span v-if="row.pricing.intervals?.length" class="pricing-detail-chip">
                {{ t('admin.modelPricing.tiers', { count: row.pricing.intervals.length }) }}
              </span>
              <span v-if="row.pricing.time_pricing?.periods?.length" class="pricing-detail-chip">
                {{ t('admin.modelPricing.timePeriods', { count: row.pricing.time_pricing.periods.length }) }}
              </span>
              <span v-if="row.pricing.fast_multiplier" class="pricing-detail-chip">Fast ×{{ row.pricing.fast_multiplier }}</span>
              <span v-if="row.pricing.flex_multiplier" class="pricing-detail-chip">Flex ×{{ row.pricing.flex_multiplier }}</span>
              <span
                v-if="!row.pricing.intervals?.length && !row.pricing.time_pricing?.periods?.length && !row.pricing.fast_multiplier && !row.pricing.flex_multiplier"
                class="text-gray-400"
              >—</span>
            </div>
          </template>

          <template #cell-actions="{ row }">
            <button type="button" class="inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-sm font-medium text-primary-600 hover:bg-primary-50 dark:text-primary-400 dark:hover:bg-primary-900/20" @click="openRowEditor(row)">
              <Icon name="edit" size="sm" />
              {{ t('common.edit') }}
            </button>
          </template>

          <template #empty>
            <EmptyState
              :title="t('admin.modelPricing.emptyTitle')"
              :description="t('admin.modelPricing.emptyDescription')"
              :action-text="t('admin.modelPricing.addRule')"
              @action="openCreateEditor"
            />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="filteredRows.length > 0"
          :page="page"
          :total="filteredRows.length"
          :page-size="pageSize"
          @update:page="page = $event"
          @update:page-size="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>

    <BaseDialog
      :show="editorOpen"
      :title="t('admin.modelPricing.editorTitle')"
      width="extra-wide"
      @close="closeEditor"
    >
      <div class="space-y-5">
        <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
          <div>
            <label class="input-label">{{ t('admin.modelPricing.editorScope') }}</label>
            <Select :model-value="editorScope" :options="editorScopeOptions" @update:model-value="changeEditorScope" />
          </div>
          <div>
            <label class="input-label">{{ t('admin.modelPricing.editorOwner') }}</label>
            <Select :model-value="editorOwnerId" :options="editorOwnerOptions" searchable @update:model-value="changeEditorOwner" />
          </div>
          <div>
            <label class="input-label">{{ t('admin.modelPricing.editorPlatform') }}</label>
            <Select
              :model-value="editorPlatform"
              :options="editorPlatformOptions"
              :disabled="editorScope === 'group'"
              @update:model-value="changeEditorPlatform"
            />
          </div>
        </div>

        <div class="flex items-start gap-3 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm dark:border-amber-900/50 dark:bg-amber-950/20">
          <Icon name="lightbulb" size="md" class="mt-0.5 flex-shrink-0 text-amber-600 dark:text-amber-400" />
          <p class="text-amber-800 dark:text-amber-200">
            {{ editorScope === 'group' ? t('admin.modelPricing.groupEditorHint') : t('admin.modelPricing.channelEditorHint') }}
          </p>
        </div>

        <div v-if="editorOwnerId" class="space-y-3">
          <PricingEntryCard
            v-for="(entry, index) in editorEntries"
            :key="`${editorPlatform}-${index}`"
            :entry="entry"
            :platform="editorPlatform"
            :hide-token-intervals="editorScope === 'group'"
            :enable-time-pricing="editorScope === 'channel'"
            enable-tier-multipliers
            @update="editorEntries.splice(index, 1, $event)"
            @remove="editorEntries.splice(index, 1)"
          />

          <button type="button" class="flex w-full items-center justify-center gap-2 rounded-xl border border-dashed border-gray-300 px-4 py-3 text-sm font-medium text-gray-600 transition-colors hover:border-primary-400 hover:bg-primary-50 hover:text-primary-600 dark:border-dark-600 dark:text-gray-300 dark:hover:border-primary-600 dark:hover:bg-primary-900/20" @click="editorEntries.push(createEmptyPricingEntry())">
            <Icon name="plus" size="sm" />
            {{ t('admin.modelPricing.addPricingEntry') }}
          </button>
        </div>

        <div v-else class="rounded-xl border border-dashed border-gray-300 py-12 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
          {{ t('admin.modelPricing.selectOwnerHint') }}
        </div>
      </div>

      <template #footer>
        <button type="button" class="btn btn-secondary" :disabled="saving" @click="closeEditor">
          {{ t('common.cancel') }}
        </button>
        <button type="button" class="btn btn-primary" :disabled="saving || !editorOwnerId" @click="saveEditor">
          <Icon v-if="saving" name="refresh" size="sm" class="mr-2 animate-spin" />
          {{ saving ? t('common.saving') : t('admin.modelPricing.saveRules', { count: editorEntries.length }) }}
        </button>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { Channel, ChannelModelPricing } from '@/api/admin/channels'
import type { AdminGroup } from '@/types'
import type { Column } from '@/components/common/types'
import type { PricingFormEntry } from '@/components/admin/channel/types'
import {
  findModelConflict,
  isValidPositiveMultiplier,
  validateIntervals,
  validateTimePricing,
} from '@/components/admin/channel/types'
import { extractApiErrorMessage } from '@/utils/apiError'
import { useAppStore } from '@/stores/app'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import PricingEntryCard from '@/components/admin/channel/PricingEntryCard.vue'
import { getPersistedPageSize, setPersistedPageSize } from '@/composables/usePersistedPageSize'
import {
  createEmptyPricingEntry,
  formatTokenPrice,
  pricingEntryFromAPI,
  pricingEntryToAPI,
  pricingPlatforms,
  replacePlatformPricing,
} from './modelPricing'

type PricingScope = 'all' | 'channel' | 'group'
type EditablePricingScope = Exclude<PricingScope, 'all'>

interface PricingRow {
  id: string
  scope: EditablePricingScope
  ownerId: number
  ownerName: string
  ownerActive: boolean
  platform: string
  models: string[]
  pricing: ChannelModelPricing
}

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const saving = ref(false)
const channels = ref<Channel[]>([])
const groups = ref<AdminGroup[]>([])
const activeScope = ref<PricingScope>('all')
const searchQuery = ref('')
const platformFilter = ref('')
const ownerFilter = ref('')
const page = ref(1)
const pageSize = ref(getPersistedPageSize())

const editorOpen = ref(false)
const editorScope = ref<EditablePricingScope>('channel')
const editorOwnerId = ref<number | string>('')
const editorPlatform = ref<string>('openai')
const editorEntries = ref<PricingFormEntry[]>([])

const columns = computed<Column[]>(() => [
  { key: 'scope', label: t('admin.modelPricing.columns.scope'), sortable: false },
  { key: 'owner', label: t('admin.modelPricing.columns.owner'), sortable: false },
  { key: 'platform', label: t('admin.modelPricing.columns.platform'), sortable: false },
  { key: 'models', label: t('admin.modelPricing.columns.models'), sortable: false },
  { key: 'mode', label: t('admin.modelPricing.columns.mode'), sortable: false },
  { key: 'prices', label: t('admin.modelPricing.columns.prices'), sortable: false },
  { key: 'advanced', label: t('admin.modelPricing.columns.advanced'), sortable: false },
  { key: 'actions', label: t('common.actions'), sortable: false },
])

const allRows = computed<PricingRow[]>(() => {
  const rows: PricingRow[] = []
  for (const channel of channels.value) {
    for (let index = 0; index < (channel.model_pricing || []).length; index += 1) {
      const pricing = channel.model_pricing[index]
      rows.push({
        id: `channel-${channel.id}-${pricing.id ?? index}`,
        scope: 'channel',
        ownerId: channel.id,
        ownerName: channel.name,
        ownerActive: channel.status === 'active',
        platform: pricing.platform || 'anthropic',
        models: pricing.models || [],
        pricing,
      })
    }
  }
  for (const group of groups.value) {
    for (let index = 0; index < (group.model_pricing || []).length; index += 1) {
      const pricing = group.model_pricing[index]
      rows.push({
        id: `group-${group.id}-${pricing.id ?? index}`,
        scope: 'group',
        ownerId: group.id,
        ownerName: group.name,
        ownerActive: group.status === 'active',
        platform: pricing.platform || group.platform,
        models: pricing.models || [],
        pricing,
      })
    }
  }
  return rows
})

const channelRowCount = computed(() => allRows.value.filter(row => row.scope === 'channel').length)
const groupRowCount = computed(() => allRows.value.filter(row => row.scope === 'group').length)
const uniqueModelCount = computed(() => new Set(allRows.value.flatMap(row => row.models.map(model => model.toLowerCase()))).size)
const wildcardRuleCount = computed(() => allRows.value.filter(row => row.models.some(model => model.endsWith('*'))).length)
const configuredOwnerCount = computed(() => new Set(allRows.value.map(row => `${row.scope}:${row.ownerId}`)).size)

const scopeTabs = computed(() => [
  { value: 'all' as const, label: t('admin.modelPricing.tabs.all'), count: allRows.value.length },
  { value: 'channel' as const, label: t('admin.modelPricing.tabs.channel'), count: channelRowCount.value },
  { value: 'group' as const, label: t('admin.modelPricing.tabs.group'), count: groupRowCount.value },
])

const platformFilterOptions = computed(() => [
  { value: '', label: t('admin.modelPricing.allPlatforms') },
  ...pricingPlatforms.map(platform => ({ value: platform, label: platformLabel(platform) })),
  { value: 'composite', label: platformLabel('composite') },
])

const visibleOwners = computed(() => {
  if (activeScope.value === 'channel') return channels.value.map(item => ({ scope: 'channel', id: item.id, name: item.name }))
  if (activeScope.value === 'group') return groups.value.map(item => ({ scope: 'group', id: item.id, name: item.name }))
  return [
    ...channels.value.map(item => ({ scope: 'channel', id: item.id, name: item.name })),
    ...groups.value.map(item => ({ scope: 'group', id: item.id, name: item.name })),
  ]
})

const ownerFilterOptions = computed(() => [
  { value: '', label: t('admin.modelPricing.allOwners') },
  ...visibleOwners.value.map(owner => ({
    value: `${owner.scope}:${owner.id}`,
    label: `${owner.scope === 'channel' ? t('admin.modelPricing.scope.channel') : t('admin.modelPricing.scope.group')} · ${owner.name}`,
  })),
])

const filteredRows = computed(() => {
  const query = searchQuery.value.toLowerCase()
  return allRows.value.filter(row => {
    if (activeScope.value !== 'all' && row.scope !== activeScope.value) return false
    if (platformFilter.value && row.platform !== platformFilter.value) return false
    if (ownerFilter.value && `${row.scope}:${row.ownerId}` !== ownerFilter.value) return false
    if (query && !row.ownerName.toLowerCase().includes(query) && !row.models.some(model => model.toLowerCase().includes(query))) return false
    return true
  })
})

const pagedRows = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredRows.value.slice(start, start + pageSize.value)
})

const editorScopeOptions = computed(() => [
  { value: 'channel', label: t('admin.modelPricing.scope.channel') },
  { value: 'group', label: t('admin.modelPricing.scope.group') },
])

const editorOwnerOptions = computed(() => {
  const source = editorScope.value === 'channel' ? channels.value : groups.value
  return source.map(owner => ({ value: owner.id, label: owner.name }))
})

const selectedEditorGroup = computed(() => groups.value.find(group => group.id === Number(editorOwnerId.value)))
const editorPlatformOptions = computed(() => {
  if (editorScope.value === 'group') {
    const platform = selectedEditorGroup.value?.platform || 'openai'
    return [{ value: platform, label: platformLabel(platform) }]
  }
  return pricingPlatforms.map(platform => ({ value: platform, label: platformLabel(platform) }))
})

watch([activeScope, searchQuery, platformFilter, ownerFilter], () => {
  page.value = 1
})

watch(activeScope, () => {
  ownerFilter.value = ''
})

watch(filteredRows, rows => {
  const maxPage = Math.max(1, Math.ceil(rows.length / pageSize.value))
  if (page.value > maxPage) page.value = maxPage
})

function platformLabel(platform: string): string {
  return t(`admin.groups.platforms.${platform}`, platform)
}

function billingModeLabel(mode: string): string {
  const key = mode === 'per_request' ? 'perRequest' : mode
  return t(`admin.channels.billingMode.${key}`, mode)
}

function formatPerRequestPrice(value: number | null | undefined): string {
  return value == null ? '—' : `$${value.toLocaleString(undefined, { maximumFractionDigits: 8 })}`
}

function setActiveScope(scope: PricingScope) {
  activeScope.value = scope
}

function handlePageSizeChange(value: number) {
  pageSize.value = value
  page.value = 1
  setPersistedPageSize(value)
}

async function fetchAllChannels(): Promise<Channel[]> {
  const result: Channel[] = []
  const batchSize = 100
  let currentPage = 1
  while (true) {
    const response = await adminAPI.channels.list(currentPage, batchSize, { sort_by: 'id', sort_order: 'asc' })
    result.push(...response.items)
    if (result.length >= response.total || response.items.length === 0) return result
    currentPage += 1
  }
}

async function loadData() {
  loading.value = true
  try {
    const [channelItems, groupItems] = await Promise.all([
      fetchAllChannels(),
      adminAPI.groups.getAllIncludingInactive(),
    ])
    channels.value = channelItems
    groups.value = groupItems
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.modelPricing.loadFailed')))
  } finally {
    loading.value = false
  }
}

function defaultEditorPlatform(scope: EditablePricingScope, ownerId: number | string): string {
  if (scope === 'group') {
    return groups.value.find(group => group.id === Number(ownerId))?.platform || 'openai'
  }
  return platformFilter.value && platformFilter.value !== 'composite' ? platformFilter.value : 'openai'
}

function syncEditorEntries() {
  const ownerId = Number(editorOwnerId.value)
  if (!ownerId) {
    editorEntries.value = []
    return
  }
  const owner = editorScope.value === 'channel'
    ? channels.value.find(channel => channel.id === ownerId)
    : groups.value.find(group => group.id === ownerId)
  editorEntries.value = (owner?.model_pricing || [])
    .filter(pricing => (pricing.platform || (editorScope.value === 'group' ? selectedEditorGroup.value?.platform : 'anthropic')) === editorPlatform.value)
    .map(pricingEntryFromAPI)
}

function openCreateEditor() {
  const requestedScope: EditablePricingScope = activeScope.value === 'group' ? 'group' : 'channel'
  const owners = requestedScope === 'channel' ? channels.value : groups.value
  if (owners.length === 0) {
    appStore.showError(t('admin.modelPricing.noOwners'))
    return
  }
  editorScope.value = requestedScope
  editorOwnerId.value = owners[0].id
  editorPlatform.value = defaultEditorPlatform(requestedScope, editorOwnerId.value)
  syncEditorEntries()
  editorEntries.value.push(createEmptyPricingEntry())
  editorOpen.value = true
}

function openRowEditor(row: PricingRow) {
  editorScope.value = row.scope
  editorOwnerId.value = row.ownerId
  editorPlatform.value = row.platform
  syncEditorEntries()
  editorOpen.value = true
}

function closeEditor() {
  if (saving.value) return
  editorOpen.value = false
  editorEntries.value = []
}

function changeEditorScope(value: string | number | boolean | null) {
  if (value !== 'channel' && value !== 'group') return
  editorScope.value = value
  const owners = value === 'channel' ? channels.value : groups.value
  editorOwnerId.value = owners[0]?.id ?? ''
  editorPlatform.value = defaultEditorPlatform(value, editorOwnerId.value)
  syncEditorEntries()
}

function changeEditorOwner(value: string | number | boolean | null) {
  if (typeof value !== 'number' && typeof value !== 'string') return
  editorOwnerId.value = value
  editorPlatform.value = defaultEditorPlatform(editorScope.value, value)
  syncEditorEntries()
}

function changeEditorPlatform(value: string | number | boolean | null) {
  if (typeof value !== 'string') return
  editorPlatform.value = value
  syncEditorEntries()
}

function validateEditor(): boolean {
  for (const entry of editorEntries.value) {
    if (entry.models.length === 0) {
      appStore.showError(t('admin.modelPricing.validation.modelsRequired'))
      return false
    }
    if (!isValidPositiveMultiplier(entry.fast_multiplier) || !isValidPositiveMultiplier(entry.flex_multiplier)) {
      appStore.showError(t('admin.modelPricing.validation.multiplierPositive'))
      return false
    }
    const intervalError = validateIntervals(entry.intervals || [], entry.billing_mode, t)
    if (intervalError) {
      appStore.showError(intervalError)
      return false
    }
    if (editorScope.value === 'channel') {
      const timeError = validateTimePricing(entry.time_pricing, t)
      if (timeError) {
        appStore.showError(timeError)
        return false
      }
    }
  }
  const conflict = findModelConflict(editorEntries.value.flatMap(entry => entry.models))
  if (conflict) {
    appStore.showError(t('admin.modelPricing.validation.conflict', { first: conflict[0], second: conflict[1] }))
    return false
  }
  return true
}

async function saveEditor() {
  const ownerId = Number(editorOwnerId.value)
  if (!ownerId || !validateEditor()) return

  saving.value = true
  try {
    if (editorScope.value === 'channel') {
      const latest = await adminAPI.channels.getById(ownerId)
      const edited = editorEntries.value.map(entry => pricingEntryToAPI(entry, editorPlatform.value))
      await adminAPI.channels.update(ownerId, {
        model_pricing: replacePlatformPricing(latest.model_pricing || [], editorPlatform.value, edited),
      })
    } else {
      const latest = await adminAPI.groups.getById(ownerId)
      const groupPlatform = latest.platform
      const edited = editorEntries.value.map(entry => pricingEntryToAPI(entry, groupPlatform, { groupOverride: true }))
      await adminAPI.groups.update(ownerId, {
        model_pricing: replacePlatformPricing(
          latest.model_pricing || [],
          editorPlatform.value,
          edited,
          groupPlatform,
        ),
      })
    }
    appStore.showSuccess(t('admin.modelPricing.saveSuccess'))
    editorOpen.value = false
    await loadData()
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.modelPricing.saveFailed')))
  } finally {
    saving.value = false
  }
}

onMounted(loadData)
</script>

<style scoped>
.pricing-stat-card {
  @apply flex min-h-28 flex-col rounded-2xl border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-800;
}

.pricing-stat-label {
  @apply text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400;
}

.pricing-stat-value {
  @apply mt-1 text-2xl font-semibold text-gray-900 dark:text-white;
}

.pricing-stat-hint {
  @apply mt-auto pt-2 text-xs text-gray-400 dark:text-gray-500;
}

.pricing-detail-chip {
  @apply rounded-md bg-gray-100 px-2 py-1 text-gray-600 dark:bg-dark-700 dark:text-gray-300;
}
</style>
