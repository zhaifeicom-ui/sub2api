<script setup lang="ts">
import { computed } from 'vue'
const props = defineProps<{ baseUrl?: string; apiKey?: string }>()
const root = computed(() => (props.baseUrl || window.location.origin).replace(/\/v1\/?$/, '').replace(/\/+$/, ''))
const auth = computed(() => `Authorization: Bearer ${props.apiKey || 'YOUR_VIDEO_KEY'}`)
const discovery = computed(() => `curl '${root.value}/v1/skills' -H '${auth.value}'`)
const submit = computed(() => `curl '${root.value}/v1/task/submit' \\\n  -H '${auth.value}' \\\n  -H 'Content-Type: application/json' \\\n  -H 'Idempotency-Key: video-order-000001' \\\n  -d '{"model":"MiniMax-H3","input":{"prompt":"晨光中的海浪，镜头缓慢推进","duration":4,"resolution":"768p","aspect_ratio":"16:9","generate_audio":false}}'`)
const query = computed(() => `curl '${root.value}/v1/task/RETURNED_STATION_TASK_ID' -H '${auth.value}'`)
const upload = computed(() => `curl '${root.value}/v1/video/assets' -H '${auth.value}' -F 'file=@reference.png'`)
</script>
<template>
 <section class="video-docs">
  <h2>视频 API 接入说明</h2><p>使用绑定“视频”分组的站内密钥。当前协议为本站异步任务 API，适配 Movo 上游；不是 OpenAI Videos 或 MiniMax 官方协议。普通聊天客户端不能直接调用。H3 的 generate_audio 不控制静音，声音由上游决定；SD 模型支持声音开关。</p>
  <h3>1. 获取当前密钥可用模型、售价和能力</h3><pre>{{ discovery }}</pre><p><code>GET /v1/models</code> 返回模型 ID；<code>GET /v1/skills</code> 或 <code>/v1/skills/guide</code> 返回完整参数、价格、限制和调用步骤。</p>
  <h3>2. 提交生成任务</h3><pre>{{ submit }}</pre><p>示例中的模型、时长和清晰度应按实际分组能力修改。返回 JSON 中的 <code>id</code> 是本站任务 ID。先用相同请求体调用 <code>POST /v1/videos/quote</code> 可获得费用；提交时可附加顶层 <code>expected_amount</code>，价格变动时拒绝提交。</p>
  <p><code>Idempotency-Key</code> 必填，长度 8–120 字符。同一内容的网络重试使用原编号；每个新任务使用新编号，防止重复扣费。报价接口不需要此头。</p>
  <h3>3. 查询结果</h3><pre>{{ query }}</pre><p>每隔 5–15 秒查询一次。<code>pending / submitting / queued / in_progress</code> 表示处理中；<code>completed</code> 后读取 <code>outputs</code> 视频地址数组；<code>failed</code> 已退款；<code>needs_review</code> 表示结果不明，金额暂时冻结，请联系管理员核对。不能拿上游任务 ID 查询本站接口。</p>
  <h3>4. 上传参考素材（可选）</h3><pre>{{ upload }}</pre><p>单文件最多 30MB、每用户素材库最多 200MB，支持 PNG/JPEG/WebP/GIF、MP4/WebM、MP3/WAV/OGG。将返回的素材 <code>id</code> 放入 <code>input.images / videos / audios</code> 数组。参数使用本站素材 ID，不接受任意远程网址。数量和声音能力以模型配置为准。</p>
  <h3>Movo 模型</h3><p>MiniMax-H3：1–15 秒、768P、按秒，总计最多 12 个参考素材；SD 2.5：4–30 秒、480P/720P/1080P、按条，最多 30 图/10 视频/10 音频，input.facs 控制普通参考图片预处理（默认开启）。当前不接入 SD 2.0。以上使用同一套本站 input 参数，由服务器转换上游格式；售价以 skills 返回值为准。当前工作台使用参考素材模式，尚不提供 Movo 首尾帧专用模式。</p>
  <h3>计费与错误</h3><p>按模型最终售价冻结余额和 Key 额度；基础计价支持按秒或按条，不叠加聊天分组倍率。高级规则可区分时长、声音和参考素材。400：参数或售价规则不匹配；401/403：密钥或访问权限问题；402：余额／额度不足；404：任务不存在或不属于此 Key；409：幂等编号冲突。</p>
  <p>结果通常是上游外链，可能过期，请及时下载。需要鉴权下载的 MP4 会保存在本站素材库（单片最多100MB，占用素材库200MB容量）。剧本工作台保留逐镜结果，不会自动拼接成片。剧本 AI 拆分使用另外的聊天 Key 并按聊天规则计费。</p>
 </section>
</template>
<style scoped>
.video-docs{max-width:1000px;margin:auto;padding:24px;color:inherit}h2{font-size:21px;font-weight:650}h3{font-size:16px;font-weight:600;margin-top:24px}p{font-size:13px;line-height:1.9;margin:10px 0;color:#64748b}pre{white-space:pre-wrap;overflow-wrap:anywhere;background:#0f172a;color:#dbeafe;padding:16px;border-radius:10px;font-size:12px;margin:12px 0}code{font-size:12px;color:#0891b2}
</style>
