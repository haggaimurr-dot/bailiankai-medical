import { defineStore } from 'pinia'
import type { components } from '../../../../packages/contracts/src/schema'
import { ApiError, request } from '../api'

export type PatientSession = components['schemas']['PatientSession']
export type PatientIdentity = components['schemas']['PatientIdentity']

const SESSION_STORAGE_KEY = 'blk-patient-session'

function loadStored(): PatientSession | null {
  try {
    const raw = window.localStorage.getItem(SESSION_STORAGE_KEY)
    if (!raw) return null
    const session = JSON.parse(raw) as PatientSession
    if (!session.token || !session.patient?.id || Date.parse(session.expiresAt) <= Date.now()) return null
    return session
  } catch {
    return null
  }
}

let creating: Promise<PatientSession> | null = null

export const useSessionStore = defineStore('session', {
  state: () => ({ session: loadStored() as PatientSession | null }),
  getters: {
    patientId: (state) => state.session?.patient.id || '',
    patient: (state) => state.session?.patient || null,
    loggedIn: (state) => Boolean(state.session),
  },
  actions: {
    persist(session: PatientSession | null) {
      this.session = session
      if (session) window.localStorage.setItem(SESSION_STORAGE_KEY, JSON.stringify(session))
      else window.localStorage.removeItem(SESSION_STORAGE_KEY)
    },
    async createSession(): Promise<PatientSession> {
      // Demo identity: the web build runs against the isolated demo
      // backend where POST /dev/patient-sessions is enabled.
      const session = await request<PatientSession>('/dev/patient-sessions', { method: 'POST', data: {} })
      this.persist(session)
      return session
    },
    async ensureSession(): Promise<PatientSession> {
      if (this.session && Date.parse(this.session.expiresAt) > Date.now()) return this.session
      if (!creating) {
        creating = this.createSession().finally(() => {
          creating = null
        })
      }
      return creating
    },
    expire() {
      this.persist(null)
    },
    cachePatient(patient: PatientIdentity) {
      if (this.session?.patient.id === patient.id) {
        this.persist({ ...this.session, patient })
      }
    },
    requireIdentity(patientId: string): PatientSession {
      if (!patientId || !this.session || this.session.patient.id !== patientId) {
        throw new ApiError('患者身份已变化，请重新加载页面。', 409, 'PATIENT_IDENTITY_CHANGED')
      }
      return this.session
    },
  },
})
