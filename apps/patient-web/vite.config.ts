import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// Dev: the patient API runs locally in development mode on :8081
// (APP_ENV=development + ENABLE_DEMO=true, isolated demo database).
// Production build is served same-origin with the API, so the app
// always calls the relative /api/v1 base.
export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5174,
    strictPort: true,
    proxy: { '/api': 'http://127.0.0.1:8081' },
  },
})
