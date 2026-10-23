import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { api, errorMessage, canEditSchedule, type ScheduleSetting, type Clinic, type Doctor, type Slot } from '../api'
import { shanghaiDate } from '../dates'

export const useScheduleStore = defineStore('schedule', () => {
  const clinics = ref<Clinic[]>([])
  const doctors = ref<Doctor[]>([])
  const slots = ref<Slot[]>([])
  const clinicId = ref('')
  const doctorId = ref('')
  const date = ref(shanghaiDate())
  const loading = ref(false)
  const initialized = ref(false)
  const error = ref('')
  const refreshedAt = ref<Date | null>(null)
  const setting = ref<ScheduleSetting | null>(null)
  const settingError = ref('')
  let settingRequest = 0
  async function loadSetting(id = clinicId.value) {
    const version = ++settingRequest
    setting.value = null; settingError.value = ''
    if (!id || !canEditSchedule.value) return
    try {
      const result = await api.scheduleSetting(id)
      if (version !== settingRequest) return
      if (result.clinicId !== id || !Array.isArray(result.templates) || !Array.isArray(result.holidays)) throw new Error('排班规则返回格式无效。')
      setting.value = result
    } catch (cause) { if (version === settingRequest) settingError.value = errorMessage(cause) }
  }
  let requestVersion = 0

  const selectedClinic = computed(() => clinics.value.find((clinic) => clinic.id === clinicId.value))
  const clinicDoctors = computed(() => doctors.value.filter((doctor) => !clinicId.value || doctor.clinicId === clinicId.value))
  const visibleSlots = computed(() => [...slots.value].sort((a, b) => a.startsAt.localeCompare(b.startsAt) || a.doctorId.localeCompare(b.doctorId)))
  const stats = computed(() => ({
    total: slots.value.length,
    open: slots.value.filter((slot) => slot.status === 'open').length,
    available: slots.value.filter((slot) => slot.status === 'open').reduce((sum, slot) => sum + Math.max(0, slot.capacity - slot.reserved), 0),
    closed: slots.value.filter((slot) => slot.status === 'closed').length,
  }))

  async function refresh() {
    const maySelectUpcomingDate = !initialized.value && date.value === shanghaiDate()
    const version = ++requestVersion
    loading.value = true
    error.value = ''
    try {
      const [nextClinics, nextDoctors] = await Promise.all([api.clinics(), api.doctors()])
      if (version !== requestVersion) return
      clinics.value = nextClinics
      doctors.value = nextDoctors
      if (!clinicId.value || !nextClinics.some((clinic) => clinic.id === clinicId.value)) {
        clinicId.value = nextClinics[0]?.id || ''
      }
      if (doctorId.value && !clinicDoctors.value.some((doctor) => doctor.id === doctorId.value)) doctorId.value = ''
      if (setting.value?.clinicId !== clinicId.value) await loadSetting()
      let nextSlots = clinicId.value ? await api.slots({ clinicId: clinicId.value, doctorId: doctorId.value, date: date.value }) : []
      if (version !== requestVersion) return
      if (maySelectUpcomingDate && clinicId.value && !nextSlots.length) {
        const upcomingSlots = await api.slots({ clinicId: clinicId.value })
        if (version !== requestVersion) return
        const now = Date.now()
        const firstUpcomingSlot = upcomingSlots
          .filter((slot) => new Date(slot.startsAt).getTime() > now)
          .sort((a, b) => new Date(a.startsAt).getTime() - new Date(b.startsAt).getTime())[0]
        if (firstUpcomingSlot) {
          date.value = shanghaiDate(new Date(firstUpcomingSlot.startsAt))
          nextSlots = await api.slots({ clinicId: clinicId.value, doctorId: doctorId.value, date: date.value })
        }
      }
      if (version !== requestVersion) return
      slots.value = nextSlots
      initialized.value = true
      refreshedAt.value = new Date()
    } catch (cause) {
      if (version !== requestVersion) return
      slots.value = []
      error.value = errorMessage(cause)
    } finally {
      if (version === requestVersion) loading.value = false
    }
  }

  async function selectClinic(value: string) {
    clinicId.value = value
    doctorId.value = ''
    await refresh()
  }

  return { clinics, doctors, slots, clinicId, doctorId, date, loading, initialized, error, refreshedAt, selectedClinic, clinicDoctors, visibleSlots, stats, refresh, selectClinic, setting, settingError, loadSetting }
})
