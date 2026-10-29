<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { showToast } from 'vant'
import { API_BASE_URL, api, errorMessage, type KnowledgeArticle, type KnowledgeQuizQuestion } from '../api'
import { patientApi } from '../patient'
import { useSessionStore } from '../stores/session'
import { dateTime } from '../utils/appointment'

const route = useRoute()
const router = useRouter()
const session = useSessionStore()

const loading = ref(true)
const error = ref('')
const article = ref<KnowledgeArticle | null>(null)
const related = ref<KnowledgeArticle[]>([])
const relatedError = ref('')
const favorite = ref<boolean | null>(null)
const favoriteError = ref('')
const favoriteBusy = ref(false)

// 阅读进度：窗口滚动位置 / 页面可滚动距离；内容一屏放得下时直接记 100%。
// 自测：每题选中的选项下标；交卷后锁定并展示每题解析。
const quizAnswers = ref<Record<number, number>>({})
const quizSubmitted = ref(false)
function quizQuestions(): KnowledgeQuizQuestion[] {
  return article.value?.quiz || []
}
function quizScore(): number {
  return quizQuestions().filter((q, i) => quizAnswers.value[i] === q.answerIndex).length
}
function submitQuiz() {
  if (quizQuestions().some((_, i) => quizAnswers.value[i] === undefined)) {
    showToast('还有题目没作答')
    return
  }
  quizSubmitted.value = true
}
function resetQuiz() {
  quizAnswers.value = {}
  quizSubmitted.value = false
}

const readProgress = ref(0)
function updateProgress() {
  const el = document.documentElement
  const max = el.scrollHeight - window.innerHeight
  readProgress.value = max > 0 ? Math.min(100, Math.max(0, Math.round(((el.scrollTop || window.scrollY) / max) * 100))) : 100
}

function dateOnly(value?: string): string {
  return value ? dateTime(value).slice(0, 10) : ''
}

// Open-event beacon (API_MAP): fire-and-forget, keepalive so it survives
// navigation; carries the patient token only when a session exists.
function sendOpenBeacon(id: string) {
  try {
    const clinicId = String(route.query.clinicId || '')
    void fetch(`${API_BASE_URL}/knowledge/articles/${encodeURIComponent(id)}/open`, {
      method: 'POST',
      keepalive: true,
      headers: {
        'Content-Type': 'application/json',
        ...(session.session ? { Authorization: `Bearer ${session.session.token}` } : {}),
      },
      body: JSON.stringify({ clinicId }),
    }).catch(() => undefined)
  } catch {
    // Beacons must never break reading.
  }
}

async function loadFavorite(id: string) {
  favorite.value = null
  favoriteError.value = ''
  if (!session.loggedIn) return
  try {
    favorite.value = await patientApi.knowledgeFavorite(id, session.patientId)
  } catch (cause) {
    favoriteError.value = errorMessage(cause)
  }
}

async function loadRelated(id: string) {
  relatedError.value = ''
  try {
    related.value = await api.knowledgeRelated(id)
  } catch (cause) {
    related.value = []
    relatedError.value = errorMessage(cause)
  }
}

async function load() {
  const id = String(route.params.id || '')
  loading.value = true
  error.value = ''
  article.value = null
  readProgress.value = 0
  quizAnswers.value = {}
  quizSubmitted.value = false
  if (!id) {
    error.value = '文章入口信息不完整，请返回科普列表。'
    loading.value = false
    return
  }
  try {
    article.value = await api.knowledgeArticle(id)
    sendOpenBeacon(id)
    await Promise.allSettled([loadRelated(id), loadFavorite(id)])
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    loading.value = false
    requestAnimationFrame(updateProgress)
  }
}

async function toggleFavorite() {
  const current = article.value
  if (!current || favoriteBusy.value) return
  if (!session.loggedIn) {
    router.push('/profile')
    return
  }
  favoriteBusy.value = true
  try {
    const target = !favorite.value
    const state = await patientApi.setKnowledgeFavorite(current.id, target, session.patientId)
    favorite.value = state
    showToast(state ? '已收藏' : '已取消收藏')
  } catch (cause) {
    favoriteError.value = errorMessage(cause)
  } finally {
    favoriteBusy.value = false
  }
}

function openRelated(item: KnowledgeArticle) {
  router.replace({ path: `/knowledge/${item.id}`, query: route.query.clinicId ? { clinicId: String(route.query.clinicId) } : {} })
}

watch(() => route.params.id, () => void load())
onMounted(() => {
  void load()
  window.addEventListener('scroll', updateProgress, { passive: true })
  updateProgress()
})
onUnmounted(() => window.removeEventListener('scroll', updateProgress))
</script>

<template>
  <main class="page">
    <div class="read-progress" role="progressbar" :aria-valuenow="readProgress" aria-valuemin="0" aria-valuemax="100" :aria-label="`阅读进度 ${readProgress}%`">
      <div class="read-progress-fill" :style="{ width: `${readProgress}%` }" />
    </div>
    <div v-if="loading" class="card"><van-loading size="24px">正在读取文章…</van-loading></div>

    <div v-else-if="error || !article" class="card">
      <h2>文章暂不可用</h2>
      <div class="error-box">{{ error || '文章入口信息不完整，请返回科普列表。' }}</div>
      <div class="row" style="margin-top: 10px">
        <button class="ghost" @click="load">重新加载</button>
        <button class="ghost" @click="router.push('/knowledge')">返回科普列表</button>
      </div>
    </div>

    <template v-else>
      <article class="card">
        <h1 class="article-title">{{ article.title }}</h1>
        <p v-if="article.summary" class="muted preline">{{ article.summary }}</p>
        <p class="muted">
          审核：{{ article.reviewerName || '—' }}<template v-if="article.reviewedAt"> · {{ dateOnly(article.reviewedAt) }}</template>
        </p>
        <p class="muted">发布：{{ dateOnly(article.publishedAt) }}（北京时间）</p>
        <p class="muted read-progress-text">阅读进度 {{ readProgress }}%</p>
        <div class="body-text selectable">{{ article.body || '' }}</div>
        <section v-if="article.videoUrl" class="video-block">
          <h2 class="source-title">短视频科普</h2>
          <video :src="article.videoUrl" controls preload="metadata" playsinline class="video-player" />
        </section>
        <section v-if="quizQuestions().length" class="quiz-block">
          <h2 class="source-title">看完测一测</h2>
          <p class="muted quiz-note">自测只做科普互动，帮助巩固刚学到的知识，不作任何诊断结论。</p>
          <div v-for="(q, qi) in quizQuestions()" :key="qi" class="quiz-q">
            <p class="quiz-question">{{ qi + 1 }}. {{ q.question }}</p>
            <label
              v-for="(opt, oi) in q.options"
              :key="oi"
              class="quiz-opt"
              :class="{
                picked: quizAnswers[qi] === oi && !quizSubmitted,
                correct: quizSubmitted && oi === q.answerIndex,
                wrong: quizSubmitted && quizAnswers[qi] === oi && oi !== q.answerIndex,
              }"
            >
              <input
                type="radio"
                :name="`quiz-${qi}`"
                :value="oi"
                :disabled="quizSubmitted"
                :checked="quizAnswers[qi] === oi"
                @change="quizAnswers[qi] = oi"
              />
              <span>{{ opt }}</span>
              <van-icon v-if="quizSubmitted && oi === q.answerIndex" name="passed" class="quiz-mark ok" />
              <van-icon v-if="quizSubmitted && quizAnswers[qi] === oi && oi !== q.answerIndex" name="close" class="quiz-mark bad" />
            </label>
            <p v-if="quizSubmitted && q.explanation" class="quiz-explain muted">解析：{{ q.explanation }}</p>
          </div>
          <div v-if="quizSubmitted" class="quiz-result">
            本次得分：{{ quizScore() }} / {{ quizQuestions().length }} ·
            <button class="ghost" @click="resetQuiz">再测一次</button>
          </div>
          <button v-else class="primary" @click="submitQuiz">提交自测</button>
        </section>
        <template v-if="article.sourceText">
          <h2 class="source-title">参考来源</h2>
          <p class="muted preline selectable">{{ article.sourceText }}</p>
        </template>
        <p class="muted disclaimer">科普内容用于了解知识，个人诊疗安排以医生面诊为准。</p>
      </article>

      <div v-if="favoriteError" class="card">
        <div class="error-box">收藏状态读取失败：{{ favoriteError }}</div>
        <button class="ghost" style="margin-top: 10px" @click="loadFavorite(article.id)">重新读取收藏状态</button>
      </div>
      <button class="primary" :disabled="favoriteBusy" @click="toggleFavorite">
        {{ favorite ? '取消收藏' : '收藏文章' }}
      </button>
      <button class="ghost" @click="router.push({ path: '/knowledge', query: { favorites: '1' } })">查看我的收藏 ›</button>

      <section class="card">
        <h2>相关内容 · 同分类推荐</h2>
        <div v-if="relatedError" class="error-box">推荐内容暂不可用：{{ relatedError }}</div>
        <button v-if="relatedError" class="ghost" style="margin-top: 10px" @click="loadRelated(article.id)">重新加载</button>
        <van-empty v-else-if="!related.length" description="暂无相关内容" />
        <article v-for="item in related" :key="item.id" class="related-item" @click="openRelated(item)">
          <strong>{{ item.title }}</strong>
          <p v-if="item.summary" class="muted preline">{{ item.summary }}</p>
          <p class="muted">审核：{{ item.reviewerName || '—' }}</p>
        </article>
      </section>
    </template>
  </main>
</template>

<style scoped>
.article-title { font-size: 20px; line-height: 1.5; margin: 0 0 8px; }
.read-progress { position: fixed; top: 0; left: 0; right: 0; height: 3px; background: rgba(23, 99, 198, 0.15); z-index: 50; }
.read-progress-fill { height: 100%; background: var(--brand); transition: width 0.1s linear; }
.read-progress-text { font-size: 12px; }
.preline { white-space: pre-line; line-height: 1.7; font-size: 13px; }
.body-text { white-space: pre-line; line-height: 1.9; font-size: 15px; margin-top: 14px; }
.selectable { user-select: text; }
.source-title { font-size: 15px; margin: 16px 0 6px; }
.disclaimer { margin-top: 16px; }
.related-item { border-top: 1px solid var(--line); padding: 10px 0; cursor: pointer; }
.video-block { margin-top: 16px; }
.video-player { width: 100%; border-radius: 10px; background: #000; max-height: 320px; }
.quiz-block { margin-top: 16px; border-top: 1px solid var(--line); padding-top: 8px; }
.quiz-note { font-size: 12px; }
.quiz-q { margin: 14px 0; }
.quiz-question { font-weight: 600; font-size: 14px; }
.quiz-opt { display: flex; align-items: center; gap: 8px; padding: 8px 10px; border: 1px solid var(--line); border-radius: 8px; margin: 6px 0; cursor: pointer; font-size: 14px; }
.quiz-opt.picked { border-color: var(--brand); background: rgba(23, 99, 198, 0.06); }
.quiz-opt.correct { border-color: #1a9e54; background: rgba(26, 158, 84, 0.08); }
.quiz-opt.wrong { border-color: #d33; background: rgba(221, 51, 51, 0.06); }
.quiz-mark.ok { color: #1a9e54; }
.quiz-mark.bad { color: #d33; }
.quiz-explain { font-size: 13px; margin: 6px 0 0; }
.quiz-result { margin: 12px 0; font-size: 14px; }
.related-item:first-of-type { border-top: 0; }
</style>
