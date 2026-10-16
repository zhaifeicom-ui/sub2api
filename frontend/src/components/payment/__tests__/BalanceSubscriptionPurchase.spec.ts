import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import BalanceSubscriptionPurchase from '../BalanceSubscriptionPurchase.vue'
import type { SubscriptionPlan } from '@/types/payment'
const { purchase, user } = vi.hoisted(() => ({ purchase: vi.fn(), user: { id: 1, balance: 20 } }))
vi.mock('@/api/payment', () => ({ paymentAPI: { purchaseSubscriptionWithBalance: purchase } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
function create() {
  return mount(BalanceSubscriptionPurchase, { props: { plan: { id: 5, price: 2, name: 'Day card' } as SubscriptionPlan }, global: { stubs: {
    BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' }
  } } })
}
beforeEach(() => { vi.clearAllMocks(); sessionStorage.clear(); user.balance = 20; vi.stubGlobal('crypto', { randomUUID: () => 'purchase-operation-0001' }) })
afterEach(() => { vi.unstubAllGlobals() })
describe('balance subscription checkout', () => {
  it('requires confirmation and reuses the operation key after an ambiguous failure', async () => {
    purchase.mockRejectedValueOnce(new Error('network error')).mockResolvedValueOnce({ data: { status: 'COMPLETED' } })
    const wrapper = create()
    await wrapper.get('[data-test="balance-subscribe"]').trigger('click')
    expect(purchase).not.toHaveBeenCalled()
    await wrapper.get('[data-test="confirm-balance-subscribe"]').trigger('click'); await flushPromises()
    expect(purchase).toHaveBeenCalledWith(5, 2, 'purchase-operation-0001')
    expect(sessionStorage.getItem('subscription-balance:1:5:2')).toBe('purchase-operation-0001')
    await wrapper.get('[data-test="confirm-balance-subscribe"]').trigger('click'); await flushPromises()
    expect(purchase).toHaveBeenLastCalledWith(5, 2, 'purchase-operation-0001')
    expect(wrapper.emitted('purchased')).toHaveLength(1)
    expect(sessionStorage.getItem('subscription-balance:1:5:2')).toBeNull()
    wrapper.unmount()
  })
  it('prevents double submission while the payment is pending', async () => {
    let resolve!: (value: unknown) => void
    purchase.mockReturnValue(new Promise(r => { resolve = r }))
    const wrapper = create()
    await wrapper.get('[data-test="balance-subscribe"]').trigger('click')
    await wrapper.get('[data-test="confirm-balance-subscribe"]').trigger('click')
    await wrapper.get('[data-test="confirm-balance-subscribe"]').trigger('click')
    expect(purchase).toHaveBeenCalledTimes(1)
    resolve({ data: {} }); await flushPromises(); wrapper.unmount()
  })
  it('disables balance purchase when funds are insufficient', () => {
    user.balance = 1
    const wrapper = create()
    expect(wrapper.get('[data-test="balance-subscribe"]').attributes('disabled')).toBeDefined()
    expect(purchase).not.toHaveBeenCalled(); wrapper.unmount()
  })
})
