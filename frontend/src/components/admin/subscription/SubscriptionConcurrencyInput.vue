<template>
  <div>
    <label class="input-label">{{ t(override ? 'common.subscriptionConcurrencyOverride' : 'common.subscriptionConcurrency') }}</label>
    <input type="number" min="0" max="100000" step="1" class="input" :value="modelValue || 0" @input="update" />
    <p class="input-hint">{{ t(override ? 'common.subscriptionConcurrencyOverrideHint' : 'common.subscriptionConcurrencyHint') }}</p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
defineProps<{ modelValue?: number; override?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: number] }>()
const { t } = useI18n()
function update(event: Event) {
  emit('update:modelValue', Number((event.target as HTMLInputElement).value))
}
</script>
