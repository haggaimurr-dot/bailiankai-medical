<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showToast } from 'vant'
import { api, errorMessage, type Clinic, type KnowledgeArticle, type KnowledgeCategory } from '../api'
import { patientApi } from '../patient'
import { useSessionStore } from '../stores/session'

const route = useRoute()
const router = useRouter()
const session = useSessionStore()

const loading = ref(true)
const loadingMore = ref(false)
const error = ref('')
const clinics = ref<Clinic[]>([])
const clinicId = ref(String(route.query.clinicId || ''))
const categories = ref<KnowledgeCategory[]>([])
const categoryId = ref('')
const keyword = ref('')
const appliedKeyword = ref('')
const articles = ref<KnowledgeArticle[]>([])

// 搜索历史：仅存本地，不上传；热门词取自已加载文章命中过的领域词，保证点选有结果。
const HISTORY_KEY = 'blk-knowledge-search-history'
const HOT_CANDIDATES = ['痘印', '痘坑', '护理', '冷敷', '保湿', '清洁', '防晒', '敏感肌', '治疗', '恢复']
function loadHistory(): string[] {
  try {
    const raw = localStorage.getItem(HISTORY_KEY)
    if (!raw) return []
    const parsed: unknown = JSON.parse(raw)
    return Array.isArray(parsed) ? parsed.filter((item): item is string => typeof item === 'string').slice(0, 8) : []
  } catch {
    return []
  }
}
const searchHistory = ref<string[]>(loadHistory())
const hotKeywords = computed(() => {
  const haystack = articles.value.map((a) => `${a.title} ${a.summary || ''}`).join('\n')
  return HOT_CANDIDATES.filter((term) => haystack.includes(term)).slice(0, 6)
})
function saveHistory(term: string) {
  const next = [term, ...searchHistory.value.filter((item) => item !== term)].slice(0, 8)
  searchHistory.value = next
  try {
    localStorage.setItem(HISTORY_KEY, JSON.stringify(next))
  } catch {
    // 历史存不下也不影响搜索。
  }
}
function clearHistory() {
  searchHistory.value = []
  try {
    localStorage.removeItem(HISTORY_KEY)
  } catch {
    // 忽略清理失败。
  }
}
const page = ref(0)
const total = ref(0)
const favoritesMode = ref(route.query.favorites === '1')
const favoriteIds = ref<Set<string>>(new Set())
const togglingId = ref('')

const hasMore = computed(() => page.value * 20 < total.value)
const selectedClinic = computed(() => clinics.value.find((c) => c.id === clinicId.value) || null)
const needsLogin = computed(() => favoritesMode.value && !session.loggedIn)
const emptyText = computed(() => {
  if (appliedKeyword.value) return '没有找到匹配文章，试试其他关键词'
  if (favoritesMode.value) return '当前院区暂无可阅读的收藏文章'
  return '暂无已发布科普内容'
})

async function loadFavoriteStates(items: KnowledgeArticle[]) {
  if (!session.loggedIn) return
  const results = await Promise.allSettled(items.map(async (a) => [a.id, await patientApi.knowledgeFavorite(a.id, session.patientId)] as const))
  const next = new Set(favoriteIds.value)
  for (const r of results) if (r.status === 'fulfilled') (r.value[1] ? next.add(r.value[0]) : next.delete(r.value[0]))
  favoriteIds.value = next
}

async function loadArticles(append = false) {
  if (needsLogin.value) {
    articles.value = []
    total.value = 0
    return
  }
  if (append) loadingMore.value = true
  error.value = ''
  try {
    const nextPage = append ? page.value + 1 : 1
    const envelope = favoritesMode.value
      ? await patientApi.knowledgeFavorites(session.patientId, {
          ...(clinicId.value ? { clinicId: clinicId.value } : {}),
          ...(categoryId.value ? { categoryId: categoryId.value } : {}),
          ...(appliedKeyword.value ? { q: appliedKeyword.value } : {}),
          page: nextPage,
        })
      : await api.knowledgeArticles(clinicId.value, categoryId.value, nextPage, 20, appliedKeyword.value)
    const items = envelope.data || []
    articles.value = append ? [...articles.value, ...items] : items
    page.value = envelope.meta?.page ?? nextPage
    total.value = envelope.meta?.total ?? articles.value.length
    if (favoritesMode.value) {
      favoriteIds.value = new Set([...favoriteIds.value, ...items.map((a) => a.id)])
    } else {
      await loadFavoriteStates(items)
    }
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    loadingMore.value = false
  }
}

async function loadCategories() {
  categories.value = []
  if (!clinicId.value) return
  try {
    categories.value = await api.knowledgeCategories(clinicId.value)
  } catch {
    categories.value = []
  }
}

async function init() {
  loading.value = true
  error.value = ''
  try {
    clinics.value = await api.clinics()
    if (!clinicId.value || !clinics.value.some((c) => c.id === clinicId.value)) {
      clinicId.value = clinics.value[0]?.id || ''
    }
    await loadCategories()
    await loadArticles()
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    loading.value = false
  }
}

async function switchClinic(id: string) {
  clinicId.value = id
  categoryId.value = ''
  page.value = 0
  await loadCategories()
  await loadArticles()
}

async function switchCategory(id: string) {
  categoryId.value = id
  page.value = 0
  await loadArticles()
}

async function search() {
  appliedKeyword.value = keyword.value.trim()
  page.value = 0
  if (appliedKeyword.value) saveHistory(appliedKeyword.value)
  await loadArticles()
}

function searchWith(term: string) {
  keyword.value = term
  void search()
}

async function toggleMode() {
  favoritesMode.value = !favoritesMode.value
  page.value = 0
  await loadArticles()
}

async function toggleFavorite(article: KnowledgeArticle) {
  if (!session.loggedIn || togglingId.value) return
  togglingId.value = article.id
  const target = !favoriteIds.value.has(article.id)
  try {
    const state = await patientApi.setKnowledgeFavorite(article.id, target, session.patientId)
    const next = new Set(favoriteIds.value)
    if (state) next.add(article.id)
    else next.delete(article.id)
    favoriteIds.value = next
    if (favoritesMode.value && !state) {
      articles.value = articles.value.filter((a) => a.id !== article.id)
      total.value = Math.max(0, total.value - 1)
    }
    showToast(state ? '已收藏' : '已取消收藏')
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    togglingId.value = ''
  }
}

function openArticle(article: KnowledgeArticle) {
  router.push({ path: `/knowledge/${article.id}`, query: clinicId.value ? { clinicId: clinicId.value } : {} })
}

onMounted(init)
</script>

<template>
  <main class="page">
    <h1>皮肤科普</h1>
    <p class="lede">了解知识，为面诊沟通做好准备。</p>

    <div class="row">
      <input v-model="keyword" class="field grow" maxlength="100" placeholder="搜索标题、摘要或正文" @keyup.enter="search" />
      <button class="ghost" @click="search">搜索</button>
    </div>
    <div v-if="hotKeywords.length || searchHistory.length" class="search-help">
      <div v-if="hotKeywords.length" class="help-row">
        <span class="muted help-label">热门搜索</span>
        <button v-for="term in hotKeywords" :key="term" class="chip help-chip" @click="searchWith(term)">{{ term }}</button>
      </div>
      <div v-if="searchHistory.length" class="help-row">
        <span class="muted help-label">搜索历史</span>
        <button v-for="term in searchHistory" :key="term" class="chip help-chip" @click="searchWith(term)">{{ term }}</button>
        <button class="text-link" @click="clearHistory">清除历史</button>
      </div>
    </div>
    <button class="ghost" @click="toggleMode">{{ favoritesMode ? '查看全部科普' : '查看我的收藏' }}</button>

    <div v-if="selectedClinic && clinics.length > 1" class="card">
      <label class="field-label" for="knowledge-clinic">当前院区</label>
      <select id="knowledge-clinic" class="field" :value="clinicId" @change="switchClinic(($event.target as HTMLSelectElement).value)">
        <option v-for="clinic in clinics" :key="clinic.id" :value="clinic.id">{{ clinic.name }}</option>
      </select>
    </div>
    <p v-else-if="selectedClinic" class="muted">当前院区：{{ selectedClinic.name }}</p>

    <div v-if="loading" class="card"><van-loading size="24px">正在读取科普内容…</van-loading></div>

    <div v-else-if="needsLogin" class="card">
      <h2>登录后查看账号收藏</h2>
      <p class="muted">收藏与患者账号绑定，登录后即可查看。</p>
      <button class="primary" @click="router.push('/profile')">前往我的资料登录 ›</button>
    </div>

    <template v-else>
      <div v-if="error" class="error-box">{{ error }}</div>
      <div class="filter-row">
        <button class="chip filter-chip" :class="{ active: categoryId === '' }" @click="switchCategory('')">全部</button>
        <button
          v-for="cat in categories"
          :key="cat.id"
          class="chip filter-chip"
          :class="{ active: categoryId === cat.id }"
          @click="switchCategory(cat.id)"
        >
          {{ cat.name }}
        </button>
      </div>

      <p class="muted">{{ favoritesMode ? '我的收藏' : '科普文章' }} · 共 {{ total }} 篇</p>
      <van-empty v-if="!articles.length && !error" :description="emptyText" />
      <div v-if="!articles.length && error" class="card">
        <button class="ghost" @click="init">重新加载</button>
      </div>

      <section v-for="article in articles" :key="article.id" class="card article-card">
        <div @click="openArticle(article)">
          <strong>{{ article.title }}</strong>
          <p v-if="article.summary" class="muted preline">{{ article.summary }}</p>
          <p class="muted">审核：{{ article.reviewerName || '—' }}</p>
          <p class="link-line">阅读全文 ›</p>
        </div>
        <button
          v-if="session.loggedIn"
          class="ghost fav-btn"
          :disabled="togglingId === article.id"
          @click="toggleFavorite(article)"
        >
          {{ favoriteIds.has(article.id) ? '取消收藏' : '收藏' }}
        </button>
      </section>

      <button v-if="hasMore" class="ghost" :disabled="loadingMore" @click="loadArticles(true)">
        {{ loadingMore ? '加载中…' : '加载更多' }}
      </button>
      <p class="muted">科普内容用于了解知识，个人诊疗安排以医生面诊为准。</p>
    </template>
  </main>
</template>

<style scoped>
.preline { white-space: pre-line; line-height: 1.6; font-size: 13px; }
.filter-row { display: flex; gap: 8px; flex-wrap: nowrap; overflow-x: auto; padding-bottom: 4px; }
.search-help { margin-top: 10px; display: grid; gap: 8px; }
.help-row { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; }
.help-label { font-size: 12px; flex: none; }
.help-chip { background: #fff; cursor: pointer; font-size: 12px; padding: 4px 11px; }
.filter-chip { background: #fff; cursor: pointer; font-size: 12px; padding: 5px 12px; white-space: nowrap; }
.filter-chip.active { color: var(--brand); border-color: var(--brand); background: #eff6ff; font-weight: 600; }
.article-card strong { font-size: 15px; line-height: 1.5; }
.link-line { color: var(--brand); font-size: 13px; margin: 6px 0 0; }
.fav-btn { margin-top: 10px; padding: 7px 14px; font-size: 13px; }
</style>
