import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import GroupQualityView from '../GroupQualityView.vue'
const mocks = vi.hoisted(() => ({ configs: vi.fn(), save: vi.fn(), accounts: vi.fn() }))
vi.mock('@/api/groupQuality', () => ({ QUALITY_MODELS: ['gpt-6-astra', 'gpt-6-sol'], qualityConfigs: mocks.configs, saveQualityConfig: mocks.save }))
vi.mock('@/api/admin/accounts', () => ({ list: mocks.accounts }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() }) }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (s: string) => s }) }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
vi.mock('@/components/quality/GroupQualityDialog.vue', () => ({ default: { template: '<div />' } }))
const options = { global: { stubs: {
  AppLayout: { template: '<main><slot /></main>' },
  BaseDialog: { props: ['show'], template: '<section v-if="show"><slot /><slot name="footer" /></section>' },
  Select: { template: '<div />' }, SearchInput: { template: '<div />' }, GroupQualityDialog: true,
  Toggle: { props: ['modelValue'], emits: ['update:modelValue'], template: '<button type="button" @click="$emit(\'update:modelValue\', !modelValue)">toggle</button>' }
} } }
beforeEach(() => {
  vi.resetAllMocks()
  mocks.configs.mockResolvedValue([{ group_id: 1, group_name: 'Test', platform: 'openai', enabled: true, models: ['gpt-6-sol'], available_models: ['gpt-6-sol'], customer_access: 'hidden', api_mode: 'responses', cooldown_seconds: 600, version: 0 }])
  mocks.accounts.mockResolvedValue({ items: [], total: 0 })
  mocks.save.mockResolvedValue({})
})
it('defaults schedules to off and ten minutes, then saves an hour without opening customer access', async () => {
  const wrapper = mount(GroupQualityView, options)
  await flushPromises()
  await wrapper.findAll('button').find(b => b.text() === 'groupQuality.edit')!.trigger('click')
  await flushPromises()
  const input = wrapper.get('#quality-auto-interval')
  expect((input.element as HTMLInputElement).value).toBe('10')
  expect(input.attributes('max')).toBe('525600')
  const toggles = wrapper.findAll('button').filter(b => b.text() === 'toggle')
  await toggles[1].trigger('click')
  await input.setValue(60)
  await wrapper.get('form').trigger('submit')
  await flushPromises()
  expect(mocks.save).toHaveBeenCalledWith(expect.objectContaining({ auto_enabled: true, auto_interval_seconds: 3600, customer_access: 'hidden', cooldown_seconds: 600 }))
  wrapper.unmount()
})
