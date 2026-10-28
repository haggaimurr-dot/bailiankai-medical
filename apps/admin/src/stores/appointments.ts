import { ref } from 'vue'
import { defineStore } from 'pinia'
import { api, ApiError, errorMessage, type Appointment } from '../api'

export const useAppointmentsStore = defineStore('appointments', () => {
  const appointments = ref<Appointment[]>([])
  const status = ref<Appointment['status'] | ''>('')
  const date = ref('')
  const doctorId = ref('')
  const clinicId = ref('')
  const page = ref(1)
  const pageSize = ref(20)
  const total = ref(0)
  const pendingTotal = ref<number | null>(null)
  const loading = ref(false)
  const refreshing = ref(false)
  const error = ref('')
  const countError = ref(false)
  const refreshedAt = ref<Date | null>(null)
  let requestVersion = 0

  async function refresh(options: { quiet?: boolean } = {}) {
    const version = ++requestVersion
    if (!options.quiet) loading.value = true
    refreshing.value = true
    error.value = ''
    const filters = { status: status.value, date: date.value, doctorId: doctorId.value, clinicId: clinicId.value, page: page.value, pageSize: pageSize.value }
    try {
      const mainRequest = api.appointments(filters)
      const countRequest = filters.status === 'pending'
        ? mainRequest
        : api.appointments({ date: filters.date, doctorId: filters.doctorId, clinicId: filters.clinicId, status: 'pending', page: 1, pageSize: 1 })
      const [listResult, countResult] = await Promise.allSettled([mainRequest, countRequest])
      if (version !== requestVersion) return
      if (countResult.status === 'fulfilled') {
        pendingTotal.value = countResult.value.meta.total
        countError.value = false
      } else {
        pendingTotal.value = null
        countError.value = true
      }
      if (listResult.status === 'rejected') throw listResult.reason
      const result = listResult.value
      const lastPage = Math.max(1, Math.ceil(result.meta.total / pageSize.value))
      if (page.value > lastPage) {
        page.value = lastPage
        await refresh(options)
        return
      }
      appointments.value = result.data
      total.value = result.meta.total
      refreshedAt.value = new Date()
    } catch (cause) {
      if (version !== requestVersion) return
      appointments.value = []
      total.value = 0
      error.value = cause instanceof ApiError && cause.status === 401
        ? '当前查看凭证已失效，请联系项目负责人更新演示配置。'
        : errorMessage(cause)
    } finally {
      if (version === requestVersion) {
        loading.value = false
        refreshing.value = false
      }
    }
  }

  async function applyFilters() {
    page.value = 1
    await refresh()
  }

  async function selectPage(value: number) {
    page.value = value
    await refresh()
  }

  return { appointments, status, date, doctorId, clinicId, page, pageSize, total, pendingTotal, loading, refreshing, error, countError, refreshedAt, refresh, applyFilters, selectPage }
})
