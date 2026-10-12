import { mount, flushPromises } from '@vue/test-utils'
import { expect, it, vi } from 'vitest'
import View from '../PromptInjectionsView.vue'
const mocks = vi.hoisted(() => ({ get: vi.fn(), update: vi.fn(), events: vi.fn() }))
vi.mock('@/api/admin/promptInjections', () => ({ getPromptInjections: mocks.get, updatePromptInjections: mocks.update, getManagedPolicyEvents: mocks.events }))
vi.mock('@/api/admin/groups', () => ({ getAllIncludingInactive: async () => [{ id: 7, name: 'Test' }], default: { getAllIncludingInactive: async () => [{ id: 7, name: 'Test' }] } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() }) }))
vi.mock('vue-i18n', async (original) => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (s: string) => s }) }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
it('saves group output replacement with version and preserves replacement whitespace', async () => {
  const rule = { id: 'r', name: 'Test', enabled: true, scope: 'group', group_ids: [7], target_id: 7, role: 'developer', position: 'prepend', content: 'old', models: [], priority: 100 }
  mocks.get.mockResolvedValue({ version: 2, rules: [rule] }); mocks.update.mockResolvedValue({ version: 3, rules: [rule] })
  const wrapper = mount(View, { global: { stubs: {
    TablePageLayout: { template: '<div><slot name="actions" /><slot name="table" /></div>' },
    DataTable: { inheritAttrs: false, props: ['data'], template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /></div></div>' },
    BaseDialog: { props: ['show'], template: '<section v-if="show"><slot /></section>' },
    GroupSelector: { template: '<div />' },
    Select: { props: ['modelValue', 'options'], emits: ['update:modelValue'], template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="o in options" :value="o.value">{{ o.label }}</option></select>' }
  } } })
  await flushPromises(); await wrapper.findAll('button').find(b => b.text() === 'admin.promptInjection.actions.edit')!.trigger('click')
  await wrapper.get('select').setValue('replace_output')
  const fields = wrapper.findAll('textarea'); await fields[0].setValue('hello'); await fields[1].setValue(' '); await fields[2].setValue('hello!')
  expect(wrapper.find('pre').text()).toBe('!')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(mocks.update).toHaveBeenCalledWith(expect.objectContaining({ version: 2, rules: [expect.objectContaining({ action: 'replace_output', match: 'hello', content: ' ', group_ids: [7] })] }))
  wrapper.unmount()
})
