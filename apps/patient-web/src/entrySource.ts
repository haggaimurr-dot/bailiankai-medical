import type { RouteLocationNormalized } from 'vue-router'

// Entry channel recorded for the funnel: only the in-site page class and a
// share marker are stored, never an external referrer or sharer identity.
// The last entry page wins until a new tracked page (or a share link) is
// visited; booking reads this value and sends it with the appointment.
const KEY = 'blk-entry-source'

const pageSources: Array<[RegExp, string]> = [
  [/^\/cases/, 'cases'],
  [/^\/doctors/, 'doctors'],
  [/^\/knowledge/, 'knowledge'],
  [/^\/$/, 'home'],
]

export function recordEntrySource(route: RouteLocationNormalized): void {
  try {
    const share = route.query.share
    if (typeof share === 'string' && share !== '') {
      localStorage.setItem(KEY, 'share')
      return
    }
    for (const [pattern, source] of pageSources) {
      if (pattern.test(route.path)) {
        localStorage.setItem(KEY, source)
        return
      }
    }
  } catch {
    // Entry tracking must never break navigation.
  }
}

export function entrySource(): string {
  try {
    return localStorage.getItem(KEY) || 'direct'
  } catch {
    return 'direct'
  }
}
