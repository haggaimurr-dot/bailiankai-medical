import { test, expect } from '@playwright/test'

test('客服工作台：接手咨询、回复、内部记录与期限跟进', async ({ page }) => {
 const employee = { id: 'agent-a', username: 'support', displayName: '客服甲', role: 'customer_service', capabilities: ['/support','/appointments'], clinicIds: ['clinic-demo'], staffId: null, active: true, version: 1 }
 let ticket = { id: 'support-a', patientId: 'patient-a', clinicId: 'clinic-demo', subject: '希望了解预约流程', category: 'booking', status: 'open', assigneeId: '', dueAt: null as string | null, version: 1, createdAt: '2026-10-07T01:00:00Z', updatedAt: '2026-10-07T01:00:00Z' }
 const events = [{ id: 'event-a', ticketId: ticket.id, kind: 'message', actorType: 'patient', content: '希望了解预约', internal: false, createdAt: '2026-10-07T01:00:00Z' }]
 const actions: any[] = []
 await page.addInitScript(value => sessionStorage.setItem('bailiankai.employee-session',JSON.stringify({token:'support-test',employee:value})),employee)
 await page.route('**/api/v1/**',async route => {
  const path = new URL(route.request().url()).pathname
  let data: unknown = []
  if(path.endsWith('/admin/session')) data = employee
  else if(path.endsWith('/support/agents')) data = [{id:employee.id,displayName:employee.displayName,clinicIds:employee.clinicIds}]
  else if(path.endsWith('/support/support-a/actions')) {
   const body = route.request().postDataJSON(); actions.push(body)
   ticket = {...ticket,version:ticket.version+1,...(body.action==='assign'?{assigneeId:body.assigneeId,dueAt:body.dueAt,status:'in_progress'}:body.action==='message'?{status:'waiting_patient'}:body.action==='followup'?{dueAt:body.dueAt}:body.action==='status'?{status:body.status}:{})}
   events.unshift({id:`event-${actions.length}`,ticketId:ticket.id,kind:body.action,actorType:'employee',content:body.content,internal:body.action!=='message',createdAt:'2026-10-07T02:00:00Z'});data=ticket
  } else if(path.endsWith('/support/support-a')) { await route.fulfill({json:{data:{ticket,events},meta:{page:1,pageSize:20,total:events.length}}});return }
  else if(path.endsWith('/admin/support')) { const query = new URL(route.request().url()).searchParams; expect(query.has('status')).toBe(false); expect(query.has('assignment')).toBe(false); await route.fulfill({json:{data:[ticket],meta:{page:1,pageSize:20,total:1}}});return }
  await route.fulfill({json:{data}})
 })
 await page.goto('/support')
 await expect(page.getByRole('heading',{name:'客服工作台',exact:true})).toBeVisible()
 await page.getByRole('cell',{name:/希望了解预约流程/}).click()
 await page.getByRole('button',{name:'接手工单（24小时内跟进）',exact:true}).click()
 await expect(page.getByRole('button',{name:'提交',exact:true})).toBeVisible()
 await page.locator('textarea').fill('您好，可在首页选择医生和面诊时间。')
 await page.getByRole('button',{name:'提交',exact:true}).click()
 await expect(page.getByText('您好，可在首页选择医生和面诊时间。',{exact:true})).toBeVisible()
 expect(actions[0].action).toBe('assign');expect(actions[1].version).toBe(2);expect(actions[1].action).toBe('message')
 await page.locator('.support-compose .el-select').first().click()
 await page.getByRole('option',{name:'内部服务记录',exact:true}).click()
 await page.locator('textarea').fill('已确认客户需要下午面诊。')
 await page.getByRole('button',{name:'提交',exact:true}).click()
 await expect(page.getByText('已确认客户需要下午面诊。',{exact:true})).toBeVisible()
 expect(actions[2].action).toBe('note');expect(events[0].internal).toBe(true)
 await page.locator('.support-compose .el-select').first().click()
 await page.getByRole('option',{name:'记录跟进并设置期限',exact:true}).click()
 await page.getByLabel('跟进期限',{exact:true}).fill('2026-12-01T10:30')
 await page.locator('textarea').fill('明日上午联系确认面诊安排。')
 await page.getByRole('button',{name:'提交',exact:true}).click()
 await expect(page.getByText('明日上午联系确认面诊安排。',{exact:true})).toBeVisible()
 expect(actions[3].dueAt).toBe('2026-12-01T02:30:00.000Z')
})

test('客服工作台：提交结果不明时保留原请求并重试',async({page})=>{
 const employee={id:'agent-a',role:'customer_service',capabilities:['/support'],clinicIds:['clinic-demo'],displayName:'客服甲'}
 const ticket={id:'support-a',patientId:'patient-a',clinicId:'clinic-demo',subject:'咨询',category:'general',status:'in_progress',assigneeId:'agent-a',dueAt:null,version:1,createdAt:'2026-10-07T01:00:00Z',updatedAt:'2026-10-07T01:00:00Z'}
 const requests:unknown[]=[]
 await page.addInitScript(value=>sessionStorage.setItem('bailiankai.employee-session',JSON.stringify({token:'support-test',employee:value})),employee)
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname
  if(path.endsWith('/admin/session'))return route.fulfill({json:{data:employee}})
  if(path.endsWith('/actions')){requests.push(route.request().postDataJSON());if(requests.length===1)return route.abort('failed');return route.fulfill({json:{data:{...ticket,version:2}}})}
  if(path.endsWith('/support/support-a'))return route.fulfill({json:{data:{ticket,events:[]},meta:{page:1,pageSize:20,total:0}}})
  if(path.endsWith('/admin/support'))return route.fulfill({json:{data:[ticket],meta:{page:1,pageSize:20,total:1}}})
  return route.fulfill({json:{data:[]}})
 })
 await page.goto('/support');await page.getByRole('cell',{name:/咨询/}).click();await page.locator('textarea').fill('回复内容');await page.getByRole('button',{name:'提交',exact:true}).click()
 await expect(page.getByText('上次提交结果待确认，请先重试或刷新记录核对。')).toBeVisible()
 await page.getByRole('button',{name:'重试上次提交',exact:true}).first().click()
 await expect(page.getByText('上次提交结果待确认，请先重试或刷新记录核对。')).toHaveCount(0)
 expect(requests).toHaveLength(2);expect(requests[0]).toEqual(requests[1])
})

test('客服工作台：自动分派、附件跨刷新重试、评分及新消息同步', async ({ page }) => {
 const employee = { id:'manager-a',role:'manager',capabilities:['/support'],clinicIds:['clinic-demo'],displayName:'主管' }
 let ticket = { id:'support-a',patientId:'patient-a',clinicId:'clinic-demo',subject:'附件咨询',category:'general',status:'open',assigneeId:'',dueAt:null,version:1,rating:0,ratingComment:'',createdAt:'2026-10-07T01:00:00Z',updatedAt:'2026-10-07T01:00:00Z' }
 let events: any[] = []
 const fileRequests: any[] = []
 await page.addInitScript(value => sessionStorage.setItem('bailiankai.employee-session',JSON.stringify({token:'support-test',employee:value})),employee)
 await page.route('**/api/v1/**',async route => {
  const path = new URL(route.request().url()).pathname
  if(path.endsWith('/admin/session')) return route.fulfill({json:{data:employee}})
  if(path.endsWith('/support/agents')) return route.fulfill({json:{data:[{id:'agent-a',displayName:'客服甲',clinicIds:['clinic-demo']}]}})
  if(path.endsWith('/actions')) {
   const body=route.request().postDataJSON()
   if(body.action==='auto_assign') ticket={...ticket,status:'in_progress',assigneeId:'agent-a',version:2}
   else {
    fileRequests.push(body)
    if(fileRequests.length===1) return route.abort('failed')
    ticket={...ticket,version:3}
    events=[{id:'event-file',ticketId:ticket.id,actorType:'employee',kind:'message',content:body.content,internal:false,createdAt:'2026-10-07T02:00:00Z',attachments:[{id:'file-a',name:body.attachments[0].name,mimeType:'application/pdf',size:16}]}]
   }
   return route.fulfill({json:{data:ticket}})
  }
  if(path.endsWith('/support/support-a')) return route.fulfill({json:{data:{ticket,events},meta:{page:1,pageSize:20,total:events.length}}})
  if(path.endsWith('/admin/support')) return route.fulfill({json:{data:[ticket],meta:{page:1,pageSize:20,total:1}}})
  return route.fulfill({json:{data:[]}})
 })
 await page.goto('/support')
 await page.getByRole('cell',{name:/附件咨询/}).click()
 await page.getByRole('button',{name:'自动分派',exact:true}).click()
 await expect(page.getByText(/负责人：客服甲/)).toBeVisible()
 await expect(page.getByLabel('添加附件',{exact:true})).toBeEnabled()
 await page.getByLabel('添加附件',{exact:true}).setInputFiles({name:'说明.pdf',mimeType:'application/pdf',buffer:Buffer.from('%PDF-1.4\nexample')})
 await page.getByRole('button',{name:'提交',exact:true}).click()
 await expect(page.getByText('上次提交结果待确认，请先重试或刷新记录核对。')).toBeVisible()
 await page.reload()
 await page.getByRole('button',{name:'重试上次提交',exact:true}).first().click()
 await expect(page.getByRole('button',{name:/下载附件：说明.pdf/})).toBeVisible()
 expect(fileRequests).toHaveLength(2)
 expect(fileRequests[0]).toEqual(fileRequests[1])
 expect(fileRequests[1].attachments[0].data).toBe(Buffer.from('%PDF-1.4\nexample').toString('base64'))
 await page.locator('textarea').fill('保留正在编辑的回复')
 ticket={...ticket,version:4,rating:5,ratingComment:'服务满意'}
 events.unshift({id:'event-new',ticketId:ticket.id,actorType:'patient',kind:'message',content:'客户的新消息',internal:false,createdAt:'2026-10-07T03:00:00Z'})
 await expect(page.getByText('客户的新消息',{exact:true})).toBeVisible({timeout:8000})
 await expect(page.getByText('客户评分：5 / 5 星',{exact:false})).toBeVisible()
 await expect(page.locator('textarea')).toHaveValue('保留正在编辑的回复')
})
