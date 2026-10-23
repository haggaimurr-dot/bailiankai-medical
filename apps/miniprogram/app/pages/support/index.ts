import { api, errorMessage, type Clinic } from '../../services/api';
import { patientApi, cachedSession, requirePatientIdentity } from '../../services/patient';
import type { components } from '../../../../../packages/contracts/src/schema';
type Ticket = components['schemas']['SupportTicket'];
type Event = components['schemas']['SupportEvent'];
type Create = components['schemas']['SupportCreate'];
type Action = components['schemas']['SupportAction'];
type Pending = { kind: 'create'; body: Create } | { kind: 'reply'; id: string; body: Action };
const labels: Record<string,string> = { open: '等待客服接手', in_progress: '正在处理', waiting_patient: '客服已回复', resolved: '已解决', closed: '已关闭' };
const categories: Create['category'][] = ['general','booking','aftercare','complaint'];
const date = (v: string) => { const d=new Date(Date.parse(v)+8*3600000); return `${d.getUTCFullYear()}-${String(d.getUTCMonth()+1).padStart(2,'0')}-${String(d.getUTCDate()).padStart(2,'0')} ${String(d.getUTCHours()).padStart(2,'0')}:${String(d.getUTCMinutes()).padStart(2,'0')}`; };
const pendingKey = (id: string) => `support-pending:${id}`;
type DisplayTicket = Ticket & { statusLabel: string; timeLabel: string };
type DisplayEvent = Event & { timeLabel: string };
type Upload = components['schemas']['SupportUpload'];
interface Data { rating: number; ratingComment: string; uploads: Upload[]; patientId: string; tickets: DisplayTicket[]; selected: DisplayTicket | null; events: DisplayEvent[]; clinics: Clinic[]; clinicIndex: number; categoryIndex: number; categoryNames: string[]; subject: string; content: string; reply: string; creating: boolean; busy: boolean; error: string; notice: string; page: number; total: number; eventPage: number; eventTotal: number; pending: boolean }
interface Methods { timer?: ReturnType<typeof setInterval>; detailGeneration: number; polling: boolean; readPending(): Pending | undefined; clearPending(): void; poll(): Promise<void>; attach(): void; removeAttachment(e: WechatMiniprogram.TouchEvent): void; download(e: WechatMiniprogram.TouchEvent): Promise<void>; rate(): Promise<void>; generation: number; load(): Promise<void>; detail(id: string,page?: number): Promise<void>; newTicket(): void; back(): void; select(e: WechatMiniprogram.TouchEvent): void; input(e: WechatMiniprogram.CustomEvent<{value:string}>): void; picker(e: WechatMiniprogram.CustomEvent<{value:string}>): void; submit(): Promise<void>; send(): Promise<void>; persist(p: Pending): Promise<void>; retry(): Promise<void>; discard(): void; more(): Promise<void>; moreEvents(): Promise<void> }
Page<Data,Methods>({
 data: { rating:5,ratingComment:'',uploads:[],patientId:'',tickets:[],selected:null,events:[],clinics:[],clinicIndex:0,categoryIndex:0,categoryNames:['一般咨询','预约咨询','术后服务','意见反馈'],subject:'',content:'',reply:'',creating:false,busy:false,error:'',notice:'',page:1,total:0,eventPage:1,eventTotal:0,pending:false },generation:0,detailGeneration:0,polling:false,
 onShow() { void this.load(); if(this.timer)clearInterval(this.timer);this.timer=setInterval(()=>{void this.poll();},3000); },
 onHide() { this.generation++;this.detailGeneration++;if(this.timer)clearInterval(this.timer);this.timer=undefined; },
 onUnload() { this.generation++;this.detailGeneration++;if(this.timer)clearInterval(this.timer); }, onPullDownRefresh() { void this.load(); },
 async load() {
  const g=++this.generation,session=cachedSession();
  if (!session) { this.setData({patientId:'',tickets:[],selected:null,events:[],uploads:[],reply:'',ratingComment:'',error:'请先到“我的资料”登录，再发起在线咨询。',busy:false});wx.stopPullDownRefresh();return; }
  if(this.data.patientId!==session.patient.id) this.setData({patientId:session.patient.id,tickets:[],selected:null,events:[],subject:'',content:'',reply:'',rating:5,ratingComment:'',uploads:[],creating:false});
  this.setData({busy:true,error:'',pending:!!wx.getStorageSync(pendingKey(session.patient.id))});
  try { const [result,clinics]=await Promise.all([patientApi.supportTickets(session.patient.id),api.clinics()]);if(g!==this.generation)return;this.setData({tickets:(result.data||[]).map(t=>({...t,statusLabel:labels[t.status],timeLabel:date(t.updatedAt)})),total:result.meta?.total||0,page:1,clinics});if(this.data.selected)await this.detail(this.data.selected.id); }
  catch(e){if(g===this.generation)this.setData({error:errorMessage(e)});}finally{if(g===this.generation)this.setData({busy:false});wx.stopPullDownRefresh();}
 },
 async detail(id,page=1) {
  const g=this.generation, dg=++this.detailGeneration, patientId=this.data.patientId;
  try { const d=await patientApi.supportDetail(id,patientId,page);if(g!==this.generation || dg!==this.detailGeneration || cachedSession()?.patient.id!==patientId)return;this.setData({selected:{...d.data!.ticket,statusLabel:labels[d.data!.ticket.status],timeLabel:date(d.data!.ticket.updatedAt)},events:(d.data!.events||[]).map(e=>({...e,timeLabel:date(e.createdAt)})),eventPage:page,eventTotal:d.meta?.total||0,creating:false}); }
  catch(e){if(g===this.generation)this.setData({error:errorMessage(e)});}
 },
 newTicket(){if(this.data.busy)return;this.setData({creating:true,selected:null,error:'',notice:''});},
 back(){if(this.data.busy)return;this.generation++;this.setData({creating:false,selected:null,events:[],error:''});void this.load();},
 select(e){if(this.data.busy)return;this.generation++;this.setData({busy:true,error:'',selected:null,events:[],reply:'',ratingComment:'',uploads:[]});void this.detail(String(e.currentTarget.dataset.id)).finally(()=>this.setData({busy:false}));},
 input(e){const field=String(e.currentTarget.dataset.field);if(field==='subject')this.setData({subject:e.detail.value});if(field==='content')this.setData({content:e.detail.value});if(field==='reply')this.setData({reply:e.detail.value});if(field==='ratingComment')this.setData({ratingComment:e.detail.value});},
 picker(e){if(e.currentTarget.dataset.field==='rating'){this.setData({rating:Number(e.detail.value)+1});return;}if(e.currentTarget.dataset.field==='clinic')this.setData({clinicIndex:Number(e.detail.value)});else this.setData({categoryIndex:Number(e.detail.value)});},
 async persist(p) {
  const patientId=this.data.patientId,g=this.generation;this.detailGeneration++;requirePatientIdentity(patientId);
  try { if(p.kind==='reply' && p.body.attachments?.length){
   const filePath=`${wx.env.USER_DATA_PATH}/support-pending-${encodeURIComponent(patientId)}.json`;
   wx.getFileSystemManager().writeFileSync(filePath,JSON.stringify(p),'utf8');wx.setStorageSync(pendingKey(patientId),{filePath});
  }else wx.setStorageSync(pendingKey(patientId),p); } catch(e) { this.setData({error:errorMessage(e)});return; }this.setData({pending:true,busy:true,error:'',notice:''});
  try { const ticket=p.kind==='create'?await patientApi.createSupport(p.body,patientId):await patientApi.supportAction(p.id,p.body,patientId);requirePatientIdentity(patientId);this.clearPending();if(g!==this.generation)return;this.setData({pending:false,subject:'',content:'',reply:'',uploads:[],ratingComment:'',notice:'已提交，消息每3秒自动刷新。'});await this.detail(ticket.id); }
  catch(e){if(g===this.generation)this.setData({error:errorMessage(e)});}finally{if(g===this.generation)this.setData({busy:false});}
 },
 async submit(){if(this.data.busy)return;if(this.data.pending){this.setData({error:'上次提交结果待确认，请先重试上次提交。'});return;}const clinic=this.data.clinics[this.data.clinicIndex];if(!clinic||!this.data.subject.trim()||!this.data.content.trim()){this.setData({error:'请选择院区，并填写标题和咨询内容。'});return;}await this.persist({kind:'create',body:{clinicId:clinic.id,category:categories[this.data.categoryIndex]!,subject:this.data.subject.trim(),content:this.data.content.trim(),requestKey:`support_${Date.now()}_${Math.random().toString(36).slice(2)}`}});},
 async send(){if(this.data.busy||!this.data.selected)return;if(this.data.pending){this.setData({error:'上次提交结果待确认，请先重试上次提交。'});return;}if(!this.data.reply.trim() && !this.data.uploads.length){this.setData({error:'请填写回复内容。'});return;}await this.persist({kind:'reply',id:this.data.selected.id,body:{action:'message',content:this.data.reply.trim(),...(this.data.uploads.length?{attachments:this.data.uploads}:{}),version:this.data.selected.version,requestKey:`reply_${Date.now()}_${Math.random().toString(36).slice(2)}`}});},
 async retry(){if(this.data.busy)return;try{const p=this.readPending();if(p)await this.persist(p);else await this.load();}catch(e){this.setData({error:errorMessage(e)});}},
 discard(){if(this.data.busy)return;wx.showModal({title:'核对后重新编辑',content:'请先刷新服务记录，核对上次内容是否已保存。确认后清除待重试请求，编辑内容仍保留。',success:r=>{if(r.confirm){this.clearPending();this.setData({pending:false});void this.load();}}});},
 readPending(){const stored=wx.getStorageSync(pendingKey(this.data.patientId)) as Pending|{filePath:string}|undefined;if(stored && 'filePath' in stored)return JSON.parse(String(wx.getFileSystemManager().readFileSync(stored.filePath,'utf8'))) as Pending;return stored;},
 clearPending(){const stored=wx.getStorageSync(pendingKey(this.data.patientId)) as {filePath?:string}|undefined;if(stored?.filePath){try{wx.getFileSystemManager().unlinkSync(stored.filePath);}catch{}}wx.removeStorageSync(pendingKey(this.data.patientId));},
 async poll(){if(this.polling||this.data.busy||!this.data.selected||this.data.eventPage!==1||cachedSession()?.patient.id!==this.data.patientId)return;this.polling=true;const total=this.data.eventTotal;try{await this.detail(this.data.selected.id);if(this.data.eventTotal>total)this.setData({notice:'收到新消息，服务记录已更新。'});}finally{this.polling=false;}},
 async rate(){if(this.data.busy||this.data.pending||!this.data.selected)return;await this.persist({kind:'reply',id:this.data.selected.id,body:{action:'rate',rating:this.data.rating,content:this.data.ratingComment.trim()||'服务评分',version:this.data.selected.version,requestKey:`rating_${Date.now()}_${Math.random().toString(36).slice(2)}`}});},
 attach(){if(this.data.busy||this.data.pending||!this.data.selected||this.data.uploads.length>=3)return;const g=this.generation;wx.chooseMessageFile({count:3-this.data.uploads.length,type:'file',extension:['jpg','jpeg','png','pdf'],success:async result=>{
  if(result.tempFiles.some(file=>file.size===0||file.size>2*1024*1024)){this.setData({error:'附件每个不超过2MB'});return;}
  this.setData({busy:true});try{const files=await Promise.all(result.tempFiles.map(file=>new Promise<Upload>((resolve,reject)=>wx.getFileSystemManager().readFile({filePath:file.path,encoding:'base64',success:r=>resolve({name:file.name,data:String(r.data)}),fail:reject}))));if(g===this.generation)this.setData({uploads:[...this.data.uploads,...files]});}catch(e){if(g===this.generation)this.setData({error:errorMessage(e)});}finally{if(g===this.generation)this.setData({busy:false});}
 }});},
 removeAttachment(e){if(this.data.busy||this.data.pending)return;this.setData({uploads:this.data.uploads.filter((_,i)=>i!==Number(e.currentTarget.dataset.index))});},
 async download(e){if(!this.data.selected)return;const patientId=this.data.patientId;try{const path=await patientApi.supportAttachment(this.data.selected.id,String(e.currentTarget.dataset.id),patientId);requirePatientIdentity(patientId);const name=String(e.currentTarget.dataset.name);if(/\.pdf$/i.test(name))wx.openDocument({filePath:path,fileType:'pdf',showMenu:true});else wx.previewImage({urls:[path]});}catch(e){this.setData({error:errorMessage(e)});}},
 async more(){if(this.data.busy)return;const g=this.generation;this.setData({busy:true});try{const result=await patientApi.supportTickets(this.data.patientId,this.data.page+1);if(g===this.generation)this.setData({tickets:[...this.data.tickets,...(result.data||[]).map(t=>({...t,statusLabel:labels[t.status],timeLabel:date(t.updatedAt)}))],page:this.data.page+1,total:result.meta?.total||0});}catch(e){if(g===this.generation)this.setData({error:errorMessage(e)});}finally{if(g===this.generation)this.setData({busy:false});}},
 async moreEvents(){if(this.data.busy||!this.data.selected)return;this.setData({busy:true});try{await this.detail(this.data.selected.id,this.data.eventPage+1);}finally{this.setData({busy:false});}}
});
