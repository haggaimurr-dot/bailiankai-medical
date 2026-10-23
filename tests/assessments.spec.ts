import { test, expect, type Page } from '@playwright/test'
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=', 'base64')
async function setup(page:Page, role='doctor', options:{lost?:boolean;conflict?:boolean;withdrawn?:boolean;configured?:boolean}={}) {
 const employee = {id:`assessment-${role}`,username:role,displayName:'测试复核员工',role,staffId:role==='doctor'?'doctor-demo-1':null,active:true,clinicIds:['clinic-test'],capabilities:['/account',...(['doctor','manager'].includes(role)?['/assessments']:[])],version:1}
 await page.addInitScript(value=>sessionStorage.setItem('bailiankai.employee-session',JSON.stringify({token:'assessment-ui-test',employee:value})),employee)
 const content = {summary:'仅工程测试的原始观察',observations:[{id:'o-1',code:'surface_irregularity',description:'合成纹理观察',region:'unknown',certainty:'unknown',evidence:[{photoId:'photo-front',angle:'front'}]}],limitations:['合成测试不能证明医学效果'],consultationAdvice:{recommended:true,reason:'测试面诊建议',preparation:[]}}
 let view:any = {task:{id:'assessment-test',patientId:'patient-test',clinicId:'clinic-test',appointmentId:'',supersedesAssessmentId:'',version:1,status:'awaiting_review',reviewerEmployeeId:role==='manager'?'':employee.id,attemptCount:1,maxAttempts:3,failureCode:'',retryable:false,createdAt:'2026-10-10T09:00:00+08:00',updatedAt:'2026-10-10T09:00:00+08:00',nextAttemptAt:null,submittedAt:'2026-10-10T09:00:00+08:00',analysisCompletedAt:'2026-10-10T09:01:00+08:00',reviewedAt:null},photos:[{id:'photo-front',assessmentId:'assessment-test',angle:'front',mediaType:'image/jpeg',sizeBytes:100,width:512,height:512,sha256:'a'.repeat(64),uploadedAt:'2026-10-10T09:00:00+08:00',qualityCheck:{status:'passed',checkerVersion:'test',thresholdProfileVersion:'test',checkedAt:'2026-10-10T09:00:00+08:00',checks:[],issues:[],retakeAdvice:[]}}],consent:{version:'photo-ai-v1',content:'测试AI用途授权',granted:true},publishedReview:null,result:{id:'result-test',assessmentId:'assessment-test',attemptId:'attempt-test',inputSnapshotId:'snapshot-test',generatedAt:'2026-10-10T09:01:00+08:00',analysis:{...content,schemaVersion:'assessment-result-v1',outcome:'observations_available',requiresHumanReview:true,model:{provider:'test',modelId:'observer',modelVersion:'1',deploymentVersion:'test-1',promptVersion:'test-1',adapterVersion:'test-1'}}},reviews:[],attempts:[]}
 if(options.withdrawn)view={...view,task:{...view.task,status:'cancelled'},consent:{...view.consent,granted:false},photos:[],result:null}
 const writes:{body:any;key:string;action:string}[]=[]
 await page.route('**/api/v1/**',async route=>{
  const request=route.request(),url=new URL(request.url()),path=url.pathname
  if(path.endsWith('/admin/session'))return route.fulfill({json:{data:employee}})
  if(path.endsWith('/assessment-capabilities'))return route.fulfill({json:{data:{qualityAvailable:options.configured!==false,modelAvailable:options.configured!==false,maxAttempts:3,consentVersion:'photo-ai-v1'}}})
  if(path.endsWith('/clinics'))return route.fulfill({json:{data:[{id:'clinic-test',name:'测试院区'}]}})
  if(path.endsWith('/admin/assessments')){const status=url.searchParams.get('status');const data=!status||view.task.status===status?[view.task]:[];return route.fulfill({json:{data,meta:{total:data.length,page:1,pageSize:20}}})}
  if(path.endsWith('/reviewers'))return route.fulfill({json:{data:[{id:'assigned-doctor',displayName:'同院区医生',clinicIds:['clinic-test']}]}})
  if(path.endsWith('/events'))return route.fulfill({json:{data:[{id:'event-test',assessmentId:'assessment-test',version:view.task.version,action:'analysis-completed',actorId:'system',detail:'',createdAt:'2026-10-10T09:01:00+08:00'}]}})
  if(path.includes('/photos/'))return route.fulfill({body:png,contentType:'image/png'})
  if(path.endsWith('/reviews')||path.endsWith('/assignment')){
   const body=request.postDataJSON(),key=request.headers()['idempotency-key']||'',action=path.endsWith('/reviews')?'reviews':'assignment';writes.push({body,key,action})
   if(options.conflict&&writes.length===1){view.task.version++;return route.fulfill({status:409,json:{error:{code:'ASSESSMENT_VERSION_CONFLICT',message:'任务已变化，请刷新核对'}}})}
   if(writes.length===1||key!==writes[0]?.key){
    view.task.version++
    if(action==='assignment')view.task.reviewerEmployeeId=body.reviewerEmployeeId
    else{const finalContent=body.decision==='rejected'?null:body.finalContent||content;const review={id:'review-'+writes.length,version:view.reviews.length+1,decision:body.decision,note:body.note,doctorName:employee.displayName,reviewedAt:'2026-10-10T09:05:00+08:00',finalContent};view.publishedReview=review;view.reviews.unshift({...review,assessmentId:'assessment-test',resultId:'result-test',reviewedByEmployeeId:employee.id,supersedesReviewId:''});view.task.status=body.decision==='rejected'?'rejected':'completed'}
   }
   if(options.lost&&writes.length===1)return route.abort('failed')
   return route.fulfill({json:{data:view}})
  }
  if(path.endsWith('/admin/assessments/assessment-test'))return route.fulfill({json:{data:view}})
  return route.fulfill({json:{data:[]}})
 })
 return {writes,view:()=>view}
}
test('服务端结果由分配医生修订，原始观察保留，患者预览读取发布快照',async({page})=>{
 const state=await setup(page);await page.goto('/assessments/assessment-test/review')
 await expect(page.getByRole('heading',{name:'医生复核与发布'})).toBeVisible()
 await page.getByText('修订后发布',{exact:true}).click();await expect(page.getByLabel('修订后发布',{exact:true})).toBeChecked()
 await page.getByLabel('观察概要',{exact:true}).fill('医生修订后的测试观察')
 await page.getByLabel('复核依据 / 更正原因',{exact:true}).fill('工程测试：核对后修订')
 await page.getByRole('button',{name:'保存复核并发布',exact:true}).click()
 await expect(page.getByRole('alert').filter({hasText:'操作已保存'})).toBeVisible()
 expect(state.writes[0]?.body.finalContent.summary).toBe('医生修订后的测试观察');expect(state.writes[0]?.key).toBeTruthy();expect(state.view().result.analysis.summary).toBe('仅工程测试的原始观察')
 await page.getByRole('button',{name:'患者结果预览'}).click();await expect(page.getByText('医生修订后的测试观察',{exact:true})).toBeVisible();await expect(page.getByText('仅工程测试的原始观察',{exact:true})).toHaveCount(0)
})
test('主管可分配同院区医生，不能发布患者结果',async({page})=>{
 const state=await setup(page,'manager');await page.goto('/assessments/assessment-test/task')
 await page.locator('.el-select').filter({has:page.getByRole('combobox',{name:'复核医生'})}).click();await page.getByRole('option',{name:'同院区医生'}).click();await page.getByRole('button',{name:'分配 / 改派医生'}).click()
 await expect(page.getByRole('button',{name:'保存复核并发布'})).toBeDisabled();expect(state.writes[0]?.body.reviewerEmployeeId).toBe('assigned-doctor');expect(state.writes[0]?.action).toBe('assignment')
})
test('保存成功但响应丢失后原请求恢复，刷新不创建第二次复核',async({page})=>{
 const state=await setup(page,'doctor',{lost:true});await page.goto('/assessments/assessment-test/review');await page.getByLabel('复核依据 / 更正原因',{exact:true}).fill('工程测试：原操作恢复')
 await page.getByRole('button',{name:'保存复核并发布'}).click();await expect(page.getByRole('button',{name:'重试原操作'})).toBeVisible();await page.reload();await expect(page.getByRole('button',{name:'重试原操作'})).toBeVisible();await page.getByRole('button',{name:'重试原操作'}).click()
 await expect(page.getByRole('button',{name:'重试原操作'})).toHaveCount(0);expect(state.writes).toHaveLength(2);expect(state.writes[0]?.key).toBe(state.writes[1]?.key);expect(state.writes[0]?.body).toEqual(state.writes[1]?.body);expect(state.view().reviews).toHaveLength(1)
})
test('版本冲突保留医生草稿，同步后以新版本提交',async({page})=>{
 const state=await setup(page,'doctor',{conflict:true});await page.goto('/assessments/assessment-test/review');await page.getByLabel('复核依据 / 更正原因',{exact:true}).fill('保留当前复核依据');await page.getByRole('button',{name:'保存复核并发布'}).click()
 await expect(page.getByRole('alert').filter({hasText:'任务已变化'})).toBeVisible();await expect(page.getByLabel('复核依据 / 更正原因',{exact:true})).toHaveValue('保留当前复核依据')
 await page.getByRole('button',{name:'同步最新版本并保留草稿'}).click();await expect(page.getByRole('button',{name:'刷新任务'})).toBeEnabled();await page.getByRole('button',{name:'保存复核并发布'}).click();await expect(page.getByRole('alert').filter({hasText:'操作已保存'})).toBeVisible();expect(state.writes[1]?.body.version).toBe(2)
})
test('未配置服务有明确状态，撤回授权不显示照片或原始结果',async({page})=>{
 await setup(page,'manager',{configured:false,withdrawn:true});await page.goto('/assessments/assessment-test/task');await expect(page.getByRole('alert').filter({hasText:'检查器或模型尚未配置'})).toBeVisible();await expect(page.getByRole('alert').filter({hasText:'AI授权已撤回'})).toBeVisible();await expect(page.locator('.private-photo')).toHaveCount(0);await expect(page.getByText('仅工程测试的原始观察',{exact:true})).toHaveCount(0)
})
test('状态筛选读取服务端分页',async({page})=>{
 await setup(page);await page.goto('/assessments');await expect(page.getByText('assessment-test',{exact:true})).toBeVisible();await page.locator('.el-select').filter({has:page.getByRole('combobox',{name:'评估状态'})}).click();await page.getByRole('option',{name:'复核完成',exact:true}).click();await expect(page.getByText('assessment-test',{exact:true})).toHaveCount(0)

})

test('护理账号不能进入评估模块',async({page})=>{await setup(page,'nurse');await page.goto('/assessments');await expect(page).toHaveURL(/\/account$/);await expect(page.getByRole('link',{name:'AI 辅助评估',exact:true})).toHaveCount(0)})
