import { test, expect } from '@playwright/test'

test('已打开的授权照片和放大预览在撤回或存储失败后清除', async ({ page }) => {
  const employee = { id: 'employee-manager', username: 'manager', displayName: '管理员', role: 'manager', clinicIds: ['clinic-test'], staffId: null, active: true, version: 1, capabilities: ['/patients', '/account'] }
  const patient = { id: 'patient-test', displayName: '图片授权测试', createdAt: '2026-10-01T09:00:00+08:00', appointmentCount: 1 }
  let photoStatus = 200
  let downloads = 0
  await page.addInitScript(value => sessionStorage.setItem('bailiankai.employee-session', JSON.stringify({ token: 'photo-test-session', employee: value })), employee)
  await page.clock.install()
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/photos/photo-test/content')) {
      downloads++
      if (photoStatus === 200) await route.fulfill({ contentType: 'image/png', body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=', 'base64') })
      else await route.fulfill({ status: photoStatus, json: { error: { code: photoStatus === 403 ? 'PHOTO_CONSENT_REQUIRED' : 'MEDIA_UNAVAILABLE', message: '照片当前无法访问' } } })
    } else if (path.endsWith('/admin/patients/patient-test')) {
      await route.fulfill({ json: { data: { patient, consultations: [], encounters: [{
        appointment: { id: 'appointment-test', startsAt: '2026-10-01T10:00:00+08:00', clinicName: '测试院区', doctorName: '测试医生', status: 'completed' },
        carePlan: { draftVersion: 0, draft: null, versions: [] },
        photos: [{ id: 'photo-test', angle: 'front', uploadedAt: '2026-10-01T09:00:00+08:00' }],
        followup: { treatment: null, treatmentRevisions: [], tasks: [] },
      }] } } })
    } else if (path.endsWith('/admin/patients')) await route.fulfill({ json: { data: [patient], total: 1, page: 1, pageSize: 20 } })
    else if (path.endsWith('/admin/session')) await route.fulfill({ json: { data: employee } })
    else await route.fulfill({ json: { data: [] } })
  })
  await page.goto('/patients?patientId=patient-test')
  await page.getByRole('tab', { name: '照片 (1)' }).click()
  await page.getByRole('button', { name: '查看授权照片' }).click()
  const photo = page.locator('.private-photo img')
  await expect(photo).toBeVisible()
  await photo.click()
  await expect(page.locator('.el-image-viewer__wrapper')).toBeVisible()
  // A successful renewal keeps a clinician's zoom preview open.
  await page.clock.runFor(10_100)
  await expect(page.locator('.el-image-viewer__wrapper')).toBeVisible()
  photoStatus = 403
  await page.clock.runFor(15_100)
  await expect(photo).toHaveCount(0)
  await expect(page.locator('.el-image-viewer__wrapper')).toHaveCount(0)
  await expect(page.locator('.private-photo')).toContainText('照片当前无法访问')
  photoStatus = 200
  await page.clock.runFor(15_100)
  await expect(photo).toBeVisible()
  photoStatus = 503
  await page.clock.runFor(15_100)
  await expect(photo).toHaveCount(0)
  photoStatus = 200
  await page.clock.runFor(15_100)
  await expect(photo).toBeVisible()
  await page.evaluate(() => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'hidden' })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await expect(photo).toHaveCount(0)
  expect(downloads).toBeGreaterThanOrEqual(4)
})
