<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import PrivatePhoto from './PrivatePhoto.vue'
const props = defineProps<{ caseId: string; beforeId: string; afterId: string; angle: string }>()
const position = ref(50), beforeReady = ref(false), afterReady = ref(false)
const ready = computed(() => beforeReady.value && afterReady.value)
watch(() => [props.caseId, props.beforeId, props.afterId], () => { position.value = 50; beforeReady.value = false; afterReady.value = false })
</script>
<template>
 <div class="case-comparison">
  <div class="comparison-stage" :class="{ unavailable: !ready }">
   <div class="comparison-layer"><PrivatePhoto :id="afterId" :case-id="caseId" source="case" alt="治疗后案例图片" @ready="afterReady = $event" /></div>
   <div class="comparison-layer" :style="{ clipPath: `inset(0 ${100-position}% 0 0)` }"><PrivatePhoto :id="beforeId" :case-id="caseId" source="case" alt="治疗前案例图片" @ready="beforeReady = $event" /></div>
   <template v-if="ready"><span class="label before">治疗前</span><span class="label after">治疗后</span><div class="divider" :style="{ left: `${position}%` }" /></template>
   <p v-else class="status" role="status">正在核验对比图片；读取失败时请刷新重试。</p>
  </div>
  <label>拖动查看治疗前后 · {{ angle }}<input v-model.number="position" type="range" min="0" max="100" step="1" :disabled="!ready" aria-label="治疗前后图片对比" /></label>
 </div>
</template>
<style scoped>
.case-comparison { margin:20px 0; max-width:760px }.comparison-stage { position:relative; height:360px; background:#f4f6f9; overflow:hidden }.comparison-layer { position:absolute; inset:0 }.comparison-layer :deep(.private-photo),.comparison-layer :deep(.el-image) { height:360px; min-height:0 }.unavailable .comparison-layer { visibility:hidden }.status { padding:24px; color:#64748b }.label { position:absolute; top:12px; padding:6px 12px; background:#ffffffdd; border-radius:6px }.before { left:12px }.after { right:12px }.divider { position:absolute; top:0; bottom:0; width:3px; background:#fff; box-shadow:0 0 3px #334155; pointer-events:none }label { display:block; margin-top:12px; color:#64748b }input { display:block; width:100%; margin-top:12px }
</style>
