<template>
  <div class="card space-y-3 p-5">
    <div class="flex items-center justify-between text-sm">
      <span>{{ t('common.balancePurchase') }}</span>
      <span>${{ balance.toFixed(2) }} USD</span>
    </div>
    <p class="input-hint">{{ t('common.balancePurchaseHint', { amount: plan.price.toFixed(2) }) }}</p>
    <button type="button" data-test="balance-subscribe" class="btn btn-primary w-full" :disabled="busy || balance < plan.price" @click="show = true">{{ t('common.balancePurchase') }} · ${{ plan.price.toFixed(2) }} USD</button>
    <p v-if="balance < plan.price" class="text-xs text-amber-600">{{ t('common.balancePurchaseInsufficient') }}</p>
    <BaseDialog :show="show" :title="t('common.balancePurchase')" width="narrow" @close="close">
      <p class="mb-3 font-medium">{{ plan.name }}</p>
      <p class="text-sm">{{ t('common.balancePurchaseHint', { amount: plan.price.toFixed(2) }) }}</p>
      <p v-if="error" role="alert" class="mt-3 text-sm text-red-600">{{ error }}</p>
      <template #footer><div class="flex justify-end gap-3">
        <button class="btn btn-secondary" :disabled="busy" @click="close">{{ t('common.cancel') }}</button>
        <button data-test="confirm-balance-subscribe" class="btn btn-primary" :disabled="busy" @click="purchase">{{ busy ? t('common.processing') : t('common.confirm') }}</button>
      </div></template>
    </BaseDialog>
  </div>
</template>
<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { paymentAPI } from '@/api/payment'
import type { SubscriptionPlan } from '@/types/payment'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { extractApiErrorMessage } from '@/utils/apiError'
const props = defineProps<{ plan: SubscriptionPlan }>()
const emit = defineEmits<{ purchased: [] }>()
const auth = useAuthStore()
const { t } = useI18n()
const balance = computed(() => Number(auth.user?.balance ?? 0))
const show = ref(false)
const busy = ref(false)
const error = ref('')
function close() { if (!busy.value) { show.value = false; error.value = '' } }
async function purchase() {
  if (busy.value) return
  busy.value = true; error.value = ''
  try {
    const storageKey = `subscription-balance:${auth.user?.id}:${props.plan.id}:${props.plan.price}`
    let key = sessionStorage.getItem(storageKey)
    if (!key) { key = crypto.randomUUID(); sessionStorage.setItem(storageKey, key) }
    await paymentAPI.purchaseSubscriptionWithBalance(props.plan.id, props.plan.price, key)
    sessionStorage.removeItem(storageKey)
    show.value = false
    emit('purchased')
  } catch (err) { error.value = extractApiErrorMessage(err) || t('common.balancePurchaseFailed') }
  finally { busy.value = false }
}
</script>
