<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { initialChannel, videoAPI, videoStatus, type VideoChannel, type VideoTask } from '@/api/videoStudio'
const emit = defineEmits<{ saved: [] }>()
const channels = ref<VideoChannel[]>([])
const form = ref(initialChannel())
const reviews = ref<VideoTask[]>([])
const busy = ref(false)
const message = ref('')
const errText = (e: unknown) => e instanceof Error ? e.message : String((e as { message?: string })?.message || '操作失败')
async function load() { try { channels.value = await videoAPI.settings(); reviews.value = await videoAPI.reviews() } catch (e) { message.value = errText(e) } }
async function save() { busy.value = true; try { await videoAPI.saveSettings(form.value); message.value = '视频配置已保存'; await load(); emit('saved') } catch (e) { message.value = errText(e) } finally { busy.value = false } }
function edit(c: VideoChannel) { form.value = JSON.parse(JSON.stringify(c)) }
async function reconcile(t: VideoTask, refund: boolean) {
  const value = window.prompt(refund ? '确认已向上游核对失败，填写退款依据（退款后不可撤销）' : '输入已在上游核实的任务 ID；系统将继续查询与结算')
  if (!value) return
  busy.value = true
  try { await videoAPI.reconcile(t.id, refund ? { refund: true, reason: value } : { upstream_id: value }); await load() } catch (e) { message.value = errText(e) } finally { busy.value = false }
}
onMounted(load)
</script>
<template>
  <section class="studio-settings">
    <header><h2>视频渠道与售价</h2><button @click="form = initialChannel()">新增分组配置</button></header>
    <p>先在账号管理添加 LaneAI 的 OpenAI API Key 账号，再绑定 OpenAI 余额分组并开启“允许图片生成”。这里的售价为最终站内余额单价，不额外叠加分组倍率；人民币成本单独记录。</p>
    <div class="channel-tabs"><button v-for="c in channels" :key="c.group_id" @click="edit(c)">分组 #{{ c.group_id }} · {{ c.enabled ? '开放' : '关闭' }}</button></div>
    <div class="config-grid">
      <label>分组 ID<input v-model.number="form.group_id" type="number" min="1" /></label>
      <label>上游账号 ID<input v-model.number="form.account_id" type="number" min="1" /></label>
      <label>本站公网地址（参考素材用）<input v-model="form.public_base_url" placeholder="https://你的站点" /></label>
      <label>上游最大并发<input v-model.number="form.max_concurrent" type="number" min="1" max="8" /></label>
    </div>
    <label><input v-model="form.enabled" type="checkbox" /> 开放此视频分组</label>
    <article v-for="m in form.models" :key="m.id">
      <h3>{{ m.id }}</h3>
      <div class="config-grid">
        <label>显示名<input v-model="m.label" /></label>
        <label>收费单位<select v-model="m.mode"><option value="second">按秒</option><option value="job">按条</option></select></label>
        <label>最短秒数<input v-model.number="m.min_seconds" type="number" min="1" max="30" /></label>
        <label>最长秒数<input v-model.number="m.max_seconds" type="number" min="1" max="30" /></label>
        <template v-for="(_, resolution) in m.rates" :key="resolution">
          <label>{{ resolution }} 售价（站内余额/{{ m.mode === 'second' ? '秒' : '条' }}）<input v-model.number="m.rates[resolution]" type="number" min="0.00000001" step="0.01" /></label>
          <label>{{ resolution }} 上游成本（人民币/{{ m.mode === 'second' ? '秒' : '条' }}）<input v-if="m.cost_cny" v-model.number="m.cost_cny[resolution]" type="number" min="0" step="0.01" /></label>
        </template>
      </div>
    </article>
    <p class="notice">初始售价只是模板，启用前请按你的站内余额单位与利润目标修改。报价按提交参数结算；参考素材附加费需先向上游确认，未确认前不要开放不确定的能力。</p>
    <button class="primary" :disabled="busy" @click="save">{{ busy ? '保存中…' : '保存配置' }}</button><p role="status">{{ message }}</p>
    <h2>待人工核对任务</h2><p>仅在提交结果不明、长时间无结果或响应异常时进入这里。请先核对上游，避免已成功生成却退款。</p>
    <article v-for="t in reviews" :key="t.id"><code>{{ t.id }}</code><p>{{ videoStatus[t.status] }} · {{ t.request.model }} · 冻结 {{ t.hold }}</p><p>{{ t.error }}</p><button :disabled="busy" @click="reconcile(t, false)">绑定上游任务 ID</button><button :disabled="busy" @click="reconcile(t, true)">核实失败后退款</button></article>
    <p v-if="!reviews.length">没有待核对任务。</p>
  </section>
</template>
<style scoped>
.studio-settings{max-width:1100px;margin:auto;padding:24px;color:var(--studio-text,#1e293b)}header,.channel-tabs{display:flex;gap:12px;align-items:center;justify-content:space-between;flex-wrap:wrap}h2{font-size:20px;font-weight:650}h3{font-weight:600;margin-bottom:12px}p{font-size:13px;line-height:1.8;color:#64748b;margin:12px 0}.config-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(210px,1fr));gap:12px}label{display:block;font-size:12px}input:not([type=checkbox]),select{display:block;width:100%;border:1px solid #cbd5e1;border-radius:8px;padding:9px;margin-top:5px;background:transparent}article{border:1px solid #e2e8f0;border-radius:12px;padding:18px;margin:18px 0}button{border:1px solid #cbd5e1;border-radius:8px;padding:8px 12px;font-size:12px}.primary{background:#2563eb;color:white;border:0}.notice{background:#fffbeb;padding:12px;border-radius:10px}button:disabled{opacity:.5}
</style>
