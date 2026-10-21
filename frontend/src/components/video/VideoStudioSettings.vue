<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { initialChannel, videoAPI, videoStatus, type VideoChannel, type VideoModel, type VideoTask } from '@/api/videoStudio'
const props = defineProps<{ groupId?: number }>()
const emit = defineEmits<{ saved: [] }>()
const channels = ref<VideoChannel[]>([])
const form = ref(initialChannel())
const reviews = ref<VideoTask[]>([])
const choices = ref<{ groups: { id: number; name: string }[]; accounts: { id: number; name: string }[] }>({ groups: [], accounts: [] })
const busy = ref(false)
const message = ref('')
const resolutions = ['480p', '720p', '768p', '1080p', '2k', '4k']
const errText = (e: unknown) => e instanceof Error ? e.message : String((e as { message?: string })?.message || '操作失败')
function edit(c: VideoChannel) { form.value = JSON.parse(JSON.stringify(c)) }
function reset() { edit(initialChannel()); if (props.groupId) form.value.group_id = props.groupId }
async function load() { try { [channels.value, reviews.value, choices.value] = await Promise.all([videoAPI.settings(), videoAPI.reviews(), videoAPI.channelChoices()]) } catch (e) { message.value = errText(e) } }
async function save() {
 busy.value = true
 try {
  await videoAPI.saveSettings(form.value); message.value = '视频配置已保存'; await load(); emit('saved')
 } catch (e) { message.value = errText(e) } finally { busy.value = false }
}
async function importModels() {
 if (!form.value.account_id) { message.value = '请先选择视频上游账号'; return }
 busy.value = true
 try {
  const incoming = await videoAPI.importCatalog(form.value.account_id)
  const old = new Map(form.value.models.map(m => [m.id, m]))
  form.value.models = incoming.map(m => {
   const prev = old.get(m.id)
   if (prev?.mode === m.mode) { for (const r of Object.keys(m.rates)) if (prev.rates[r] !== undefined) m.rates[r] = prev.rates[r]!; m.rules = prev.rules }
   return m
  })
  form.value.enabled = false
  message.value = '已读取此密钥可用的视频模型与人民币成本，尚未保存。已有相同计费单位的售价和高级规则保留（高级规则成本需手动核对）；新模型售价暂按成本填入。请核对利润、价格规则后保存并开启渠道。'
 } catch (e) { message.value = errText(e) } finally { busy.value = false }
}
function addModel() {
 form.value.models.push({ id: `new-video-${Date.now()}`, label: '新视频模型', mode: 'second', rates: { '768p': 0 }, cost_cny: { '768p': 0 }, min_seconds: 1, max_seconds: 15, max_images: 0, max_videos: 0, max_audios: 0, audio: false })
}
function setResolution(m: VideoModel, res: string, event: Event) {
 if ((event.target as HTMLInputElement).checked) { m.rates[res] = 0; (m.cost_cny ||= {})[res] = 0 }
 else { delete m.rates[res]; if (m.cost_cny) delete m.cost_cny[res] }
}
const fees = [
 { rate: 'base_fee', cost: 'base_cost_cny', label: '每任务起步附加费' },
 { rate: 'image_fee', cost: 'image_cost_cny', label: '每张参考图附加费' },
 { rate: 'video_fee', cost: 'video_cost_cny', label: '每个参考视频附加费' },
 { rate: 'audio_fee', cost: 'audio_cost_cny', label: '每个参考音频附加费' }
] as const
function addRule(m: VideoModel) {
 (m.rules ||= []).push({ resolution: Object.keys(m.rates)[0] || '768p', min_seconds: m.min_seconds, max_seconds: m.max_seconds, audio: 'any', reference: 'any', mode: m.mode, rate: Object.values(m.rates)[0] || 1, cost_cny: Object.values(m.cost_cny || {})[0] || 0, minimum_seconds: 0, step_seconds: 1 })
}
async function reconcile(t: VideoTask, refund: boolean) {
 const value = window.prompt(refund ? '确认已向上游核对失败，填写退款依据（退款后不可撤销）' : '输入已在上游核实的任务 ID；系统将继续查询与结算')
 if (!value) return
 busy.value = true
 try { await videoAPI.reconcile(t.id, refund ? { refund: true, reason: value } : { upstream_id: value }); await load() } catch (e) { message.value = errText(e) } finally { busy.value = false }
}
onMounted(async () => { await load(); const existing = channels.value.find(c => c.group_id === props.groupId); if (existing) edit(existing); else reset() })
</script>
<template>
 <section class="studio-settings">
  <header><h2>视频渠道与售价</h2><button type="button" @click="reset">新增视频渠道</button></header>
  <p>在账号管理选择“视频 · LaneAI 异步任务”，创建视频余额分组。保存此配置时会绑定所选账号。这里填写最终站内余额售价，不额外叠加聊天分组倍率。上游成本以人民币单独记录。</p>
  <div class="channel-tabs"><button type="button" v-for="c in channels" :key="c.group_id" @click="edit(c)">{{ choices.groups.find(g => g.id === c.group_id)?.name || `分组 #${c.group_id}` }} · {{ c.enabled ? '开放' : '关闭' }}</button></div>
  <div class="config-grid">
   <label>视频余额分组<select v-model.number="form.group_id" :disabled="!!props.groupId"><option :value="0">请选择视频分组</option><option v-for="g in choices.groups" :key="g.id" :value="g.id">{{ g.name }}</option></select></label>
   <label>视频上游账号<select v-model.number="form.account_id"><option :value="0">请选择视频上游账号</option><option v-for="a in choices.accounts" :key="a.id" :value="a.id">{{ a.name }}</option></select></label>
   <label>上游协议<select disabled><option>LaneAI 异步任务 · laneai_task</option></select></label>
   <label>本站公网地址（参考素材用）<input v-model="form.public_base_url" placeholder="https://你的站点" /></label>
   <label>上游最大并发<input v-model.number="form.max_concurrent" type="number" min="1" max="8" /></label>
  </div>
  <div class="channel-tabs"><button type="button" :disabled="busy" @click="importModels">读取上游模型与成本（不发起生成）</button><button type="button" @click="addModel">手动添加模型</button></div>
  <label><input v-model="form.enabled" type="checkbox" /> 核对售价后，开放此视频分组</label>
  <article v-for="(m, i) in form.models" :key="i">
   <header><h3>{{ m.label || m.id }}</h3><button type="button" @click="form.models.splice(i, 1)">移除模型</button></header>
   <div class="config-grid">
    <label>准确调用名<input v-model="m.id" /></label><label>显示名<input v-model="m.label" /></label>
    <label>收费单位<select v-model="m.mode"><option value="second">按秒</option><option value="job">按条（每次任务）</option></select></label>
    <label>最短秒数<input v-model.number="m.min_seconds" type="number" min="1" max="600" /></label><label>最长秒数<input v-model.number="m.max_seconds" type="number" min="1" max="600" /></label>
    <label>最多参考图<input v-model.number="m.max_images" type="number" min="0" max="9" /></label><label>最多参考视频<input v-model.number="m.max_videos" type="number" min="0" max="3" /></label><label>最多参考音频<input v-model.number="m.max_audios" type="number" min="0" max="3" /></label>
   </div>
   <label><input v-model="m.audio" type="checkbox" /> 支持生成声音</label>
   <div class="channel-tabs"><label v-for="r in resolutions" :key="r"><input type="checkbox" :checked="m.rates[r] !== undefined" @change="setResolution(m, r, $event)" /> {{ r }}</label></div>
   <div class="config-grid"><template v-for="(_, resolution) in m.rates" :key="resolution">
    <label>{{ resolution }} 售价（站内余额/{{ m.mode === 'second' ? '秒' : '条' }}）<input v-model.number="m.rates[resolution]" type="number" min="0.00000001" step="any" /></label>
    <label>{{ resolution }} 上游成本（人民币/{{ m.mode === 'second' ? '秒' : '条' }}）<input v-if="m.cost_cny" v-model.number="m.cost_cny[resolution]" type="number" min="0" step="any" /></label>
   </template></div>
   <details><summary>高级定价：时长档位、声音、参考素材、起步价及计费步长</summary>
    <p>不添加档位时，使用上面的清晰度价格。添加后，每个可售组合必须恰好匹配一个档位；无匹配或条件重叠会拒绝生成。附加费按参考素材数量计算。</p>
    <button type="button" @click="addRule(m)">添加定价档位</button>
    <article v-for="(rule, index) in m.rules" :key="index">
     <header><h3>档位 {{ index + 1 }}</h3><button type="button" @click="m.rules?.splice(index, 1)">删除档位</button></header>
     <div class="config-grid">
      <label>清晰度<select v-model="rule.resolution"><option v-for="res in Object.keys(m.rates)" :key="res">{{ res }}</option></select></label>
      <label>时长从（秒，含）<input v-model.number="rule.min_seconds" type="number" :min="m.min_seconds" :max="m.max_seconds" /></label>
      <label>时长至（秒，含）<input v-model.number="rule.max_seconds" type="number" :min="m.min_seconds" :max="m.max_seconds" /></label>
      <label>生成声音<select v-model="rule.audio"><option value="any">不限</option><option value="on">有声</option><option value="off">无声</option></select></label>
      <label>参考素材<select v-model="rule.reference"><option value="any">不限</option><option value="with">有参考素材</option><option value="without">没有参考素材</option></select></label>
      <label>收费单位<select v-model="rule.mode"><option value="second">按秒</option><option value="job">按条</option></select></label>
      <label>档位售价（站内余额/{{ rule.mode === 'second' ? '秒' : '条' }}）<input v-model.number="rule.rate" type="number" min="0.00000001" step="any" /></label>
      <label>档位成本（人民币/{{ rule.mode === 'second' ? '秒' : '条' }}）<input v-model.number="rule.cost_cny" type="number" min="0" step="any" /></label>
      <template v-if="rule.mode === 'second'"><label>最低计费秒数（0 表示按实际）<input v-model.number="rule.minimum_seconds" type="number" min="0" max="600" /></label><label>计费步长（秒，向上取整）<input v-model.number="rule.step_seconds" type="number" min="0" max="600" /></label></template>
      <template v-for="fee in fees" :key="fee.rate"><label>{{ fee.label }} · 售价<input v-model.number="rule[fee.rate]" type="number" min="0" step="any" /></label><label>{{ fee.label }} · 人民币成本<input v-model.number="rule[fee.cost]" type="number" min="0" step="any" /></label></template>
     </div>
    </article>
   </details>
  </article>
  <p class="notice">读取成本不会自动保存或开放售卖。新价格结构无法确定时会拒绝导入。按参考视频时长、Token 或未知上游实际用量收费的模型尚不能自动计价，确认并适配前请勿开放。</p>
  <button type="button" class="primary" :disabled="busy" @click="save">{{ busy ? '处理中…' : '保存配置' }}</button><p role="status">{{ message }}</p>
  <h2>待人工核对任务</h2><p>请先核对上游结果，再继续查询或退款。</p>
  <article v-for="t in reviews" :key="t.id"><code>{{ t.id }}</code><p>{{ videoStatus[t.status] }} · {{ t.request.model }} · 冻结 {{ t.hold }}</p><p>{{ t.error }}</p><button type="button" :disabled="busy" @click="reconcile(t, false)">绑定上游任务 ID</button><button type="button" :disabled="busy" @click="reconcile(t, true)">核实失败后退款</button></article><p v-if="!reviews.length">没有待核对任务。</p>
 </section>
</template>
<style scoped>
.studio-settings{max-width:1100px;margin:auto;padding:24px;color:var(--studio-text,#1e293b)}header,.channel-tabs{display:flex;gap:12px;align-items:center;justify-content:space-between;flex-wrap:wrap;margin:12px 0}h2{font-size:20px;font-weight:650}h3{font-weight:600;margin-bottom:12px}p{font-size:13px;line-height:1.8;color:#64748b;margin:12px 0}.config-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(210px,1fr));gap:12px}label{display:block;font-size:12px}input:not([type=checkbox]),select,textarea{display:block;width:100%;border:1px solid #cbd5e1;border-radius:8px;padding:9px;margin-top:5px;background:transparent}textarea{font-family:monospace;font-size:12px}article{border:1px solid #e2e8f0;border-radius:12px;padding:18px;margin:18px 0}button{border:1px solid #cbd5e1;border-radius:8px;padding:8px 12px;font-size:12px}.primary{background:#2563eb;color:white;border:0}.notice{background:#fffbeb;padding:12px;border-radius:10px}button:disabled{opacity:.5}details{margin-top:16px}summary{cursor:pointer;font-size:13px}
</style>
