<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { showToast } from 'vant'
import { errorMessage } from '../api'
import { patientApi, type FollowupTask } from '../patient'
import { useSessionStore } from '../stores/session'
import { recoverySeries, type RecoverySeries } from '../utils/recovery-calendar'

const session = useSessionStore()
const loading = ref(true)
const error = ref('')
const tasks = ref<FollowupTask[]>([])
const seriesId = ref('')
const detailId = ref('')

const series = computed<RecoverySeries[]>(() => recoverySeries(tasks.value))
const current = computed(() => series.value.find(s => s.appointmentId === seriesId.value) || series.value[0] || null)

const W = 340, H = 200, PAD = 28
function x(i: number, n: number) { return n <= 1 ? W / 2 : PAD + (i * (W - PAD * 2)) / (n - 1) }
function y(sev: number) { return H - PAD - (sev / 3) * (H - PAD * 2) }
const pathD = computed(() => {
  const c = current.value
  if (!c || !c.points.length) return ''
  return c.points.map((p, i) => `${i === 0 ? 'M' : 'L'}${x(i, c.points.length).toFixed(1)},${y(p.severity).toFixed(1)}`).join(' ')
})
const isAbnormal = (p: { severity: number; concern: boolean }) => p.concern || p.severity >= 2
const detail = computed(() => current.value?.points.find(p => p.id === detailId.value) || null)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const all: FollowupTask[] = []
    let page = 1
    for (;;) {
      const r = await patientApi.followups('', page, session.patient?.id)
      const list = r.data || []
      all.push(...list)
      if (list.length < 20) break
      page++
      if (page > 10) break
    }
    tasks.value = all
    if (series.value.length && !seriesId.value) seriesId.value = series.value[0].appointmentId
  } catch (cause) { error.value = errorMessage(cause) }
  finally { loading.value = false }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <h2>恢复趋势图</h2>
    <p class="desc">同一预约多次反馈的不适程度对照，点击异常点查看详情。</p>
    <div v-if="loading" class="loading">加载中…</div>
    <div v-else-if="error" class="error">{{ error }}<button @click="load">重试</button></div>
    <div v-else-if="!series.length" class="empty">暂无已提交的反馈，提交后可查看趋势。</div>
    <div v-else>
      <div class="tabs">
        <button v-for="s in series" :key="s.appointmentId" :class="{ active: (current && s.appointmentId === current.appointmentId) }" @click="seriesId = s.appointmentId; detailId = ''">{{ s.label }}</button>
      </div>
      <div v-if="current" class="chart-card">
        <svg :viewBox="`0 0 ${W} ${H}`" class="chart">
          <line v-for="g in [0,1,2,3]" :key="g" :x1="PAD" :x2="W - PAD" :y1="y(g)" :y2="y(g)" class="grid" />
          <text v-for="g in [0,1,2,3]" :key="'t'+g" :x="4" :y="y(g) + 4" class="axis">{{ ['无','轻微','明显','严重'][g] }}</text>
          <path :d="pathD" class="line" />
          <g v-for="(p, i) in current.points" :key="p.id">
            <circle :cx="x(i, current.points.length)" :cy="y(p.severity)" r="7" :class="['dot', isAbnormal(p) ? 'bad' : 'ok']" @click="detailId = p.id" />
            <text :x="x(i, current.points.length)" :y="H - 6" class="xlabel">{{ p.date.slice(5, 10) }}</text>
          </g>
        </svg>
        <p class="hint">● 绿色为正常反馈，红色为异常（不适明显/有担忧），点击红点查看详情</p>
      </div>
      <div v-if="detail" class="detail-card">
        <h3>{{ detail.date }} · {{ detail.severityLabel }}</h3>
        <p><strong>反馈内容：</strong>{{ detail.title }}</p>
        <p v-if="detail.symptoms"><strong>症状：</strong>{{ detail.symptoms }}</p>
        <p v-if="detail.response"><strong>医护回复：</strong>{{ detail.response }}</p>
        <p v-if="detail.disposition"><strong>处置：</strong>{{ detail.disposition }}</p>
        <button class="ghost" @click="detailId = ''">收起</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.page { padding: 16px; max-width: 640px; margin: 0 auto; }
.desc { color: #666; font-size: 13px; margin: 4px 0 12px; }
.tabs { display: flex; gap: 8px; overflow-x: auto; margin-bottom: 12px; }
.tabs button { white-space: nowrap; padding: 8px 12px; border: 1px solid #ddd; border-radius: 16px; background: #fff; font-size: 13px; }
.tabs button.active { background: #07c160; color: #fff; border-color: #07c160; }
.chart-card { background: #fff; border: 1px solid #eee; border-radius: 12px; padding: 12px; }
.chart { width: 100%; height: auto; }
.grid { stroke: #eee; stroke-width: 1; }
.axis { font-size: 10px; fill: #999; }
.xlabel { font-size: 10px; fill: #999; text-anchor: middle; }
.line { fill: none; stroke: #07c160; stroke-width: 2; }
.dot { cursor: pointer; }
.dot.ok { fill: #07c160; }
.dot.bad { fill: #ee0a24; }
.hint { font-size: 12px; color: #999; margin: 8px 0 0; }
.detail-card { margin-top: 12px; background: #fff8f0; border: 1px solid #f5d9a8; border-radius: 12px; padding: 12px 14px; }
.detail-card h3 { margin: 0 0 8px; font-size: 15px; }
.detail-card p { margin: 6px 0; font-size: 14px; color: #444; }
.ghost { margin-top: 8px; padding: 8px 16px; border: 1px solid #ddd; border-radius: 8px; background: #fff; }
.loading, .empty { color: #999; padding: 16px 0; }
.error { color: #c00; }
</style>
