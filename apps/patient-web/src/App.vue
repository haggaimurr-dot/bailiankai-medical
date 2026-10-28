<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useMessageStore } from './stores/messages'

const route = useRoute()
const router = useRouter()
const messageStore = useMessageStore()
const activeTab = computed(() => String(route.meta.tab || ''))
const showTabs = computed(() => Boolean(route.meta.tab))
const showBack = computed(() => !route.meta.tab && route.path !== '/')
const unreadLabel = computed(() => (messageStore.unreadCount > 99 ? '99+' : String(messageStore.unreadCount)))

onMounted(() => void messageStore.refresh())
watch(() => route.path, () => void messageStore.refresh())

const tabs = [
  { key: 'home', label: '首页', icon: 'wap-home-o', path: '/' },
  { key: 'doctors', label: '医生', icon: 'friends-o', path: '/doctors' },
  { key: 'appointments', label: '预约', icon: 'calendar-o', path: '/appointments' },
  { key: 'messages', label: '消息', icon: 'chat-o', path: '/messages' },
  { key: 'profile', label: '我的', icon: 'contact-o', path: '/profile' },
]
</script>

<template>
  <div class="shell">
    <header class="topbar">
      <button v-if="showBack" class="back" aria-label="返回" @click="router.back()">‹</button>
      <div class="brand">
        <span class="brand-mark">✚</span>
        <span class="brand-name">百莲凯</span>
        <span class="brand-sub">医疗美容 · 患者端</span>
      </div>
      <span class="demo-badge">演示版</span>
    </header>
    <RouterView />
    <nav v-if="showTabs" class="tabbar" aria-label="主导航">
      <button
        v-for="tab in tabs"
        :key="tab.key"
        class="tab"
        :class="{ active: activeTab === tab.key }"
        @click="router.push(tab.path)"
      >
        <span class="tab-icon">
          <van-icon :name="tab.icon" size="22" />
          <span v-if="tab.key === 'messages' && messageStore.unreadCount > 0" class="tab-badge">{{ unreadLabel }}</span>
        </span>
        <span>{{ tab.label }}</span>
      </button>
    </nav>
  </div>
</template>
