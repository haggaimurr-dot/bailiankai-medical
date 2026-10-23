import { createApp } from 'vue'
import { createPinia } from 'pinia'
import {
  ElAlert, ElButton, ElDatePicker, ElDialog, ElDrawer, ElEmpty, ElForm, ElFormItem,
  ElInput, ElInputNumber, ElLoading, ElRadioGroup, ElRadioButton, ElSkeleton, ElTag, ElOption, ElPagination, ElSelect, ElSwitch, ElTable, ElTableColumn,
} from 'element-plus'
import 'element-plus/dist/index.css'
import '../../../packages/design-tokens/mlk.web.css'
import './style.css'
import App from './App.vue'
import { router } from './router'

const app = createApp(App).use(createPinia()).use(router).use(ElLoading)
const components = { ElAlert, ElButton, ElDatePicker, ElDialog, ElDrawer, ElEmpty, ElForm, ElFormItem, ElInput, ElInputNumber, ElRadioGroup, ElRadioButton, ElSkeleton, ElTag, ElOption, ElPagination, ElSelect, ElSwitch, ElTable, ElTableColumn }
for (const [name, component] of Object.entries(components)) app.component(name, component)
app.mount('#app')
