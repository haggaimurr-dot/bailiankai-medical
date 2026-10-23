import { test, expect, type Page } from '@playwright/test'

const employee = {id:'arrival-desk-employee',username:'desk',displayName:'到诊客服',role:'customer_service',clinicIds:['clinic-desk'],staffId:null,active:true,version:1,capabilities:['/appointments','/arrivals','/doctors','/account','appointment.review']}
const fixture = () => ({id:'desk-appointment',patientId:'desk-patient',slotId:'desk-slot',clinicId:'clinic-desk',clinicName:'演示院区',doctorId:'doctor-desk',doctorName:'演示医生',startsAt:'2099-10-07T14:00:00+08:00',endsAt:'2099-10-07T14:30:00+08:00',contactName:'虚构患者',contactPhone:'13800000000',note:'',status:'confirmed',rejectionReason:'',confirmationDeadline:'2099-10-07T14:00:00+08:00',createdAt:'2026-10-01T10:00:00+08:00',updatedAt:'2026-10-01T10:00:00+08:00',version:2,arrivedAt:null as string | null,canCancel:false,canReview:false,canRegisterArrival:true,canReschedule:false,slotStatus:'open',suspensionReason:''})

async function setup(page: Page, mode: 'scan' | 'move' | 'review' = 'scan') {
 let row = fixture()
 if (mode === 'move') row = {...row,slotStatus:'closed',suspensionReason:'医生临时停诊',canReschedule:true}
 if (mode === 'review') row = {...row,status:'pending',canReview:true,canRegisterArrival:false}
 const submissions: Record<string, unknown>[] = []
 await page.addInitScript(account => sessionStorage.setItem('bailiankai.employee-session',JSON.stringify({token:'desk-test-session',employee:account})),employee)
 await page.route('**/api/v1/**',async route => {
  const url = new URL(route.request().url()), path = url.pathname
  if (path.endsWith('/admin/session')) return route.fulfill({json:{data:employee}})
  if (path.endsWith('/clinics')) return route.fulfill({json:{data:[{id:'clinic-desk',name:'演示院区'}]}})
  if (path.endsWith('/doctors')) return route.fulfill({json:{data:[{id:'doctor-desk',clinicId:'clinic-desk',name:'演示医生'}]}})
  if (path.endsWith('/slots')) return route.fulfill({json:{data:[{id:'desk-new-slot',clinicId:'clinic-desk',doctorId:'doctor-desk',startsAt:'2099-10-08T14:00:00+08:00',endsAt:'2099-10-08T14:30:00+08:00',capacity:3,reserved:0,status:'open',version:1}]}})
  if (path.endsWith('/arrival-scans/resolve')) return route.fulfill({json:{data:row}})
  if (path.endsWith('/arrival-scans/confirm')) {submissions.push(route.request().postDataJSON());row = {...row,arrivedAt:new Date().toISOString(),canRegisterArrival:false,version:3};return route.fulfill({json:{data:row}})}
  if (path.endsWith('/suspension-reschedule')) {submissions.push(route.request().postDataJSON());row = {...row,slotStatus:'open',slotId:'desk-new-slot',startsAt:'2099-10-08T14:00:00+08:00',canReschedule:false,version:3};return route.fulfill({json:{data:row}})}
  if (path.endsWith('/review')) {submissions.push(route.request().postDataJSON());row = {...row,status:'confirmed',canReview:false};return route.fulfill({json:{data:row}})}
  if (path.endsWith('/admin/appointments')) return route.fulfill({json:{data:[row],meta:{page:1,pageSize:20,total:1}}})
  if (path.endsWith('/admin/appointments/desk-appointment')) return route.fulfill({json:{data:row}})
  return route.fulfill({json:{data:[]}})
 })
 await page.goto(mode === 'review' ? '/appointments' : '/arrivals')
 return submissions
}

test('客服扫码先核对，确认后更新到诊且可扫描下一位',async ({page}) => {
 const submissions = await setup(page)
 await expect(page.getByRole('heading',{name:'到诊工作台'})).toBeVisible()
 await page.getByRole('textbox',{name:'签到码'}).fill('BLK-CHECKIN:'+ 'a'.repeat(64))
 await page.getByRole('button',{name:'核对签到码'}).click()
 await expect(page.getByRole('button',{name:'核对无误，确认到诊'})).toBeVisible()
 expect(submissions).toHaveLength(0)
 await page.getByRole('button',{name:'核对无误，确认到诊'}).click()
 await expect(page.getByText('已签到：',{exact:false})).toBeVisible()
 expect(submissions).toHaveLength(1)
 await page.getByRole('button',{name:'扫描下一位'}).click()
 await expect(page.getByRole('textbox',{name:'签到码'})).toHaveValue('')
})

test('停诊改期必须选择时段并填写沟通记录',async ({page}) => {
 const submissions = await setup(page,'move')
 await page.getByRole('button',{name:'全部停诊待改期'}).click()
 await page.getByRole('button',{name:'安排改期'}).click()
 const dialog = page.getByRole('dialog',{name:'停诊改期'})
 await expect(dialog.getByRole('button',{name:'确认改期'})).toBeDisabled()
 await dialog.getByRole('combobox').focus()
 await dialog.getByRole('combobox').press('Enter')
 await page.getByRole('option',{name:/2099-10-08/}).click()
 await dialog.getByRole('textbox').fill('电话沟通，患者同意新的就诊时间')
 await dialog.getByRole('button',{name:'确认改期'}).click()
 await expect(dialog).not.toBeVisible()
 expect(submissions).toHaveLength(1)
 expect(submissions[0]).toMatchObject({slotId:'desk-new-slot',version:2,reason:'电话沟通，患者同意新的就诊时间'})
})

test('客服审批按钮与服务端能力一致',async ({page}) => {
 const submissions = await setup(page,'review')
 await page.getByTestId('appointment-detail-desk-appointment').click()
 const dialog = page.getByRole('dialog',{name:'预约详情'})
 await expect(dialog.getByRole('button',{name:'确认预约',exact:true})).toBeEnabled()
 await dialog.getByRole('button',{name:'确认预约',exact:true}).click()
 expect(submissions).toEqual([{action:'confirm',version:2}])
})
