import { defineStore } from 'pinia'
import { patientApi } from '../patient'
import { useSessionStore } from './session'

/** Unread in-app message count, shown as a badge on the 消息 tab. */
export const useMessageStore = defineStore('messages', {
  state: () => ({ unreadCount: 0 }),
  actions: {
    async refresh() {
      const session = useSessionStore().session
      if (!session) {
        this.unreadCount = 0
        return
      }
      try {
        const result = await patientApi.patientMessageUnreadCount(session.patient.id)
        this.unreadCount = result.unreadCount
      } catch {
        // Badge is best-effort; the inbox page itself surfaces errors.
      }
    },
    markOneRead() {
      this.unreadCount = Math.max(0, this.unreadCount - 1)
    },
  },
})
