<script setup lang="ts">
defineProps<{ name: 'calendar' | 'doctors' | 'refresh' | 'arrow' | 'clock' | 'check' | 'layers' | 'pause' | 'location' | 'chevron' }>()
const paths: Record<string, string[]> = {
  calendar: ['M8 2v4M16 2v4M3 10h18', 'M5 4h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2Z', 'M7 14h3M14 14h3M7 18h3'],
  doctors: ['M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2', 'M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8Z', 'M17 4a4 4 0 0 1 0 8M22 21v-2a4 4 0 0 0-3-3.87'],
  refresh: ['M20 7v5h-5M4 17v-5h5', 'M5.6 7a7 7 0 0 1 11.55-2.6L20 7M4 17l2.85 2.6A7 7 0 0 0 18.4 17'],
  arrow: ['M5 12h14M12 5l7 7-7 7'],
  clock: ['M12 8v4l3 2', 'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20Z'],
  check: ['M9 12l2 2 4-4', 'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20Z'],
  layers: ['m12 2 10 5-10 5L2 7l10-5ZM2 12l10 5 10-5M2 17l10 5 10-5'],
  pause: ['M9 8v8M15 8v8', 'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20Z'],
  location: ['M20 10c0 6-8 12-8 12S4 16 4 10a8 8 0 0 1 16 0Z', 'M12 13a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z'],
  chevron: ['m9 5 7 7-7 7'],
}
</script>

<template>
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <path v-for="(path, index) in paths[name]" :key="index" :d="path" />
  </svg>
</template>
