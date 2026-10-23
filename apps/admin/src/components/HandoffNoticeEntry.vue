<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { api } from '../api'

const unread = ref<number | null>(null), unavailable = ref(false)
let timer: ReturnType<typeof setInterval> | undefined, active = true, loading = false
async function refresh() {
  if (loading || document.visibilityState !== 'visible') return
  loading = true
  try {
    const result = await api.handoffNotifications(1, 1)
    if (active) { unread.value = result.meta.unreadTotal; unavailable.value = false }
  } catch { if (active) { unavailable.value = true; unread.value = null } }
  finally { loading = false }
}
onMounted(() => { void refresh(); timer = setInterval(refresh, 30000); window.addEventListener('handoff-notifications-changed', refresh); document.addEventListener('visibilitychange', refresh) })
onBeforeUnmount(() => { active = false; if (timer) clearInterval(timer); window.removeEventListener('handoff-notifications-changed', refresh); document.removeEventListener('visibilitychange', refresh) })
</script>

<template>
  <RouterLink to="/workbench" class="handoff-notice-entry" :title="unavailable ? '通知暂不可用，进入工作台重试' : '查看交接通知与超时升级提醒'">交接通知<span v-if="unread !== null && unread > 0" class="unread-badge" :aria-label="`${unread} 条未读通知`">{{ unread > 99 ? '99+' : unread }}</span></RouterLink>
</template>

<style scoped>
.handoff-notice-entry { display: inline-flex; align-items: center; gap: 6px; color: var(--el-color-primary); white-space: nowrap; }
.unread-badge { background: #bd604c; color: #fff; font-size: 10px; line-height: 18px; padding: 0 6px; border-radius: 10px; min-width: 18px; text-align: center; }
</style>
