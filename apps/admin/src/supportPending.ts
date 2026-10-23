import type { components } from '../../../packages/contracts/src/schema'

export type PendingSupport = { id: string; body: components['schemas']['SupportAction'] }

async function database() {
  return new Promise<IDBDatabase>((resolve, reject) => {
    const request = indexedDB.open('bailiankai-support', 1)
    request.onupgradeneeded = () => request.result.createObjectStore('pending')
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(new Error('无法保存待重试附件，请检查浏览器存储空间'))
  })
}

async function stored(key: string, value?: PendingSupport, remove = false) {
  const db = await database()
  try {
    return await new Promise<PendingSupport | undefined>((resolve, reject) => {
      const tx = db.transaction('pending', value || remove ? 'readwrite' : 'readonly')
      const store = tx.objectStore('pending')
      // Vue draft objects can be reactive proxies, which IndexedDB cannot clone.
      const request = value ? store.put(JSON.parse(JSON.stringify(value)), key) : remove ? store.delete(key) : store.get(key)
      tx.oncomplete = () => resolve(value || remove ? undefined : request.result)
      tx.onerror = tx.onabort = () => reject(new Error('无法保存待重试附件，请检查浏览器存储空间'))
    })
  } finally { db.close() }
}

export async function readPendingSupport(key: string): Promise<PendingSupport | undefined> {
  const value = JSON.parse(sessionStorage.getItem(key) || 'null')
  return value?.stored ? await stored(key) : value || undefined
}

export async function writePendingSupport(key: string, value?: PendingSupport) {
  if (value?.body.attachments?.length) {
    await stored(key, value)
    sessionStorage.setItem(key, JSON.stringify({ stored: true }))
  } else if (value) sessionStorage.setItem(key, JSON.stringify(value))
  else {
    // Text requests do not require IndexedDB, including legacy pending requests.
    if (JSON.parse(sessionStorage.getItem(key) || 'null')?.stored) await stored(key, undefined, true)
    sessionStorage.removeItem(key)
  }
}
