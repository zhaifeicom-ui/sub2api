<template>
  <section v-if="configs.length" class="card mt-5">
    <h2 class="mb-3 text-sm font-semibold">{{ t('groupQuality.title') }}</h2>
    <div class="flex flex-wrap gap-2"><button v-for="config in configs" :key="config.group_id" class="btn btn-secondary btn-sm" @click="target = config">{{ config.group_name }} · {{ t('groupQuality.test') }}</button></div>
    <GroupQualityDialog v-if="target" :show="true" :config="target" :admin="auth.isAdmin" @close="target = null" />
  </section>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { qualityConfigs, type QualityConfig } from '@/api/groupQuality'
import GroupQualityDialog from './GroupQualityDialog.vue'
const { t } = useI18n()
const auth = useAuthStore()
const configs = ref<QualityConfig[]>([])
const target = ref<QualityConfig | null>(null)
onMounted(async () => {
  // V1 monitor IDs are not group IDs. Keep explicit group entries rather than
  // guessing identity from mutable/duplicate monitor or group names.
  try { configs.value = (await qualityConfigs(auth.isAdmin)).filter(c => c.enabled) }
  catch { configs.value = [] }
})
</script>
