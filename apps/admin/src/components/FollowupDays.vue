<script setup lang="ts">
import { computed, ref } from 'vue'
const props = defineProps<{ modelValue: number[]; disabled?: boolean; treatedAt?: string }>()
const emit = defineEmits<{ 'update:modelValue': [number[]] }>()
const day = ref<number | undefined>(undefined), notice = ref('')
const sorted = computed(() => [...props.modelValue].sort((a, b) => a - b))
function add() {
  if (!Number.isInteger(day.value) || !day.value || day.value < 1 || day.value > 90) { notice.value = '请输入 1–90 的整数天数'; return }
  if (props.modelValue.includes(day.value)) { notice.value = '该天数已添加'; return }
  if (props.modelValue.length >= 8) { notice.value = '最多配置 8 次随访'; return }
  emit('update:modelValue', [...props.modelValue, day.value].sort((a, b) => a - b)); day.value = undefined; notice.value = ''
}
function date(offset: number) {
  if (!props.treatedAt) return ''
  const value = new Date(props.treatedAt + '+08:00')
  if (!Number.isFinite(value.getTime())) return ''
  value.setTime(value.getTime() + offset * 86400000)
  return new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', month: '2-digit', day: '2-digit' }).format(value)
}
</script>
<template>
  <div class="days-editor">
    <div class="days-tags"><el-tag v-for="value in sorted" :key="value" :closable="!disabled" @close="emit('update:modelValue', modelValue.filter(item => item !== value))">第 {{ value }} 天{{ date(value) ? ` · ${date(value)}` : '' }}</el-tag><span v-if="!sorted.length">请添加至少一次随访</span></div>
    <div class="days-add"><el-input-number v-model="day" :disabled="disabled" :min="1" :max="90" :precision="0" placeholder="天数" aria-label="新增随访天数" /><el-button :disabled="disabled || modelValue.length >= 8" @click="add">添加天数</el-button><small>{{ modelValue.length }}/8 次</small></div>
    <p v-if="notice" role="alert">{{ notice }}</p><small>治疗后 1–90 天，按天数排序；具体安排由医护确认。</small>
  </div>
</template>
<style scoped>.days-editor{width:100%}.days-tags,.days-add{display:flex;gap:8px;flex-wrap:wrap;margin-bottom:10px;align-items:center}.days-editor small{color:#8391a5}.days-editor p{color:#bf4c4c}</style>
