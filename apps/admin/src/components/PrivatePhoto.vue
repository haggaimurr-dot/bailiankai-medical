<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElImage } from 'element-plus'
import { api, errorMessage } from '../api'

const props = defineProps<{ id: string; source: 'care' | 'followup' | 'case' | 'assessment'; caseId?: string; assessmentId?: string; alt: string }>()
const emit = defineEmits<{ ready: [value: boolean] }>()
const url = ref('')
const error = ref('')
let generation = 0
let disposed = false
let controller: AbortController | undefined
let timer: ReturnType<typeof setTimeout> | undefined
let expiryTimer: ReturnType<typeof setTimeout> | undefined

function release() {
  emit('ready', false)
  generation++
  controller?.abort()
  if (timer) clearTimeout(timer)
  if (expiryTimer) clearTimeout(expiryTimer)
  if (url.value) URL.revokeObjectURL(url.value)
  url.value = ''
}

async function refresh() {
  if (disposed || document.visibilityState !== 'visible') { release(); return }
  const request = ++generation
  controller?.abort()
  const currentController = new AbortController()
  controller = currentController
  const requestTimeout = setTimeout(() => currentController.abort(), 5_000)
  error.value = ''
  try {
    const next = await (props.source === 'assessment' ? api.assessmentPhotoFile(props.assessmentId || '', props.id, currentController.signal) : props.source === 'case' ? api.casePhotoFile(props.caseId || '', props.id, currentController.signal) : props.source === 'care' ? api.carePhotoFile(props.id, currentController.signal) : api.followupPhotoFile(props.id, currentController.signal))
    if (disposed || request !== generation || document.visibilityState !== 'visible') { URL.revokeObjectURL(next); return }
    // Photo ids are immutable. Renew the lease without interrupting an open
    // zoom viewer when the same photo is still authorized.
    if (url.value) URL.revokeObjectURL(next)
    else url.value = next
    if (expiryTimer) clearTimeout(expiryTimer)
    expiryTimer = setTimeout(() => { release(); void refresh() }, 15_000)
    timer = setTimeout(() => void refresh(), 10_000)
  } catch (cause) {
    if (request === generation && !disposed) {
      const timedOut = currentController.signal.aborted
      release()
      error.value = timedOut ? '图片读取超时，请稍后重试。' : errorMessage(cause)
      timer = setTimeout(() => void refresh(), 15_000)
    }
  } finally {
    clearTimeout(requestTimeout)
  }
}

function visibilityChanged() { if (document.visibilityState === 'visible') void refresh(); else release() }
watch(() => [props.id, props.source, props.caseId, props.assessmentId], () => { release(); void refresh() })
onMounted(() => { document.addEventListener('visibilitychange', visibilityChanged); void refresh() })
onBeforeUnmount(() => { disposed = true; release(); document.removeEventListener('visibilitychange', visibilityChanged) })
</script>

<template>
  <div class="private-photo">
    <ElImage v-if="url" :src="url" :alt="alt" :preview-src-list="[url]" fit="contain" @load="emit('ready', true)" @error="release(); error = '图片显示失败，请刷新后重试。'" />
    <p v-else role="status">{{ error || '正在核验照片访问权限…' }}</p>
  </div>
</template>

<style scoped>
.private-photo { width: 100%; min-height: 200px; background: #f4f6f9; }
.private-photo :deep(.el-image) { width: 100%; height: 205px; }
.private-photo p { padding: 20px; color: #64748b; font-size: 13px; }
</style>
