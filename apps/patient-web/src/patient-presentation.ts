import { showImagePreview, showToast} from 'vant'
import type { components} from '../../../packages/contracts/src/schema'
import { api, type Clinic, type Doctor, type Envelope, type KnowledgeArticle, type KnowledgeCategory} from './api'

export type ClinicPresentation = components['schemas']['ClinicPresentation']
export type DoctorPresentation = components['schemas']['DoctorPresentation']
export type Credential = components['schemas']['Credential']

// Explicitly enabled for this mock delivery; set false when replacing demo data.
export const ENABLE_HOME_MOCK = true

// Only the existing demo IDs receive mock material. Published presentation wins.
export function clinicPresentation(clinic: Clinic): ClinicPresentation {
if (clinic.presentation) return {...clinic.presentation, credentials: clinic.presentation.credentials || []}
if (ENABLE_HOME_MOCK && clinic.id === 'clinic-demo') {
return {
isDemo: true, brandName: '百莲凯医疗美容', tagline: '用心沟通 · 陪伴每一步', logoUrl: '',
introduction: '专注皮肤面诊、个体方案沟通与恢复随访。本段为品牌介绍示例，正式服务范围和机构信息以院方核验资料为准。',
hours: '09:00–18:00（演示）',
transport: '地图使用上海市中心示例坐标，演示公共交通与停车说明；此处并非真实院址。',
arrivalGuide: '请先在“我的预约”确认预约结果和时段。到院后向前台出示预约信息，再按工作人员指引候诊。',
location: { latitude: 31.2304, longitude: 121.4737},
credentials: [{ name: '医疗机构执业许可证（样例）', number: 'MOCK-CLINIC-001', issuer: '演示发证机构', imageUrl: ''}],
}
}
return { isDemo: true, brandName: clinic.name, tagline: '', introduction: '', logoUrl: '', hours: '', transport: '', arrivalGuide: '', credentials: []}
}

export function doctorPresentation(doctor: Doctor): DoctorPresentation {
return doctor.presentation
? {...doctor.presentation, credentials: doctor.presentation.credentials || []}
: {
isDemo: true,
credentials: ENABLE_HOME_MOCK && /^doctor-demo-[12]$/.test(doctor.id)
? [{ name: '医师执业证书（样例）', number: `MOCK-${doctor.id}`, issuer: '演示发证机构', imageUrl: ''}]
: [],
}
}

export function validLocation(location?: ClinicPresentation['location']): location is NonNullable<ClinicPresentation['location']> {
return!!location && Number.isFinite(location.latitude) && Number.isFinite(location.longitude) &&
Math.abs(location.latitude) <= 90 && Math.abs(location.longitude) <= 180
}

/** Web equivalent of the miniprogram's wx.previewImage credential preview. */
export function previewCredentials(credentials: Credential[], index: number) {
const current = credentials[index]?.imageUrl
if (!current) return
const urls = credentials.map((row) => row.imageUrl).filter(Boolean)
try {
showImagePreview({ images: urls, startPosition: Math.max(0, urls.indexOf(current))})
} catch {
showToast('证照图片暂时无法打开')
}
}

// ---- Mock knowledge content (ported from the miniprogram's
// services/knowledge.ts): mock only fills a successful empty response for
// the existing demo clinic. Network failures remain visible. Formal
// published articles always take priority.
const mockCategory: KnowledgeCategory = { id: 'mock-visit', clinicId: 'clinic-demo', name: '就诊准备（演示）', sortOrder: 0, active: true, version: 1}
const mockArticles: KnowledgeArticle[] = [
{
id: 'mock-knowledge-1', categoryId: mockCategory.id, title: '面诊前，如何整理沟通问题（演示）', summary: '科普栏目样例：记录关注的问题、既往就诊和用药信息。',
body: '\n\n可以将希望与医生沟通的问题整理成清单，并准备既往就诊、治疗及用药信息。\n\n到院前查看预约确认结果和时间；具体诊疗建议以医生面诊为准。',
sortOrder: 0, reviewerName: '演示审核人（虚构）', reviewedAt: '2026-10-01T09:00:00+08:00', publishedAt: '2026-10-01T10:00:00+08:00',
},
{
id: 'mock-knowledge-2', categoryId: mockCategory.id, title: '认识面诊、方案与随访流程（演示）', summary: '科普栏目样例：了解从预约到查看个人方案、填写随访反馈的流程。',
body: '\n\n提交面诊预约后，等待工作人员确认。面诊后，可在预约详情查看已发布的个人方案。\n\n治疗后的随访安排可在恢复日历中查看。本文是产品流程样例，不提供具体治疗或用药建议。',
sortOrder: 1, reviewerName: '演示审核人（虚构）', reviewedAt: '2026-10-01T09:00:00+08:00', publishedAt: '2026-10-01T10:00:00+08:00',
},
]

export const knowledge = {
async categories(clinicId: string, allowMock = true): Promise<KnowledgeCategory[]> {
const rows = await api.knowledgeCategories(clinicId)
// Only display a mock category when the public article list is also empty.
if (!ENABLE_HOME_MOCK ||!allowMock || clinicId!== 'clinic-demo' || rows.length) return rows
const articles = await api.knowledgeArticles(clinicId, '', 1, 1)
return articles.meta?.total || articles.data?.length? rows: [mockCategory]
},
async articles(clinicId: string, categoryId = '', page = 1, pageSize = 20, allowMock = true, q = ''): Promise<Envelope<KnowledgeArticle[]>> {
const result = await api.knowledgeArticles(clinicId, categoryId.startsWith('mock-')? '': categoryId, page, pageSize, q)
if (ENABLE_HOME_MOCK && allowMock && clinicId === 'clinic-demo' && (!categoryId || categoryId === mockCategory.id) && (result.meta?.total?? result.data?.length?? 0) === 0) {
if (q) {
const all = await api.knowledgeArticles(clinicId, '', 1, 1)
if (all.meta?.total || all.data?.length) return result
}
const matches = mockArticles.filter((row) =>!q || [row.title, row.summary, row.body].some((text) => text?.toLowerCase().includes(q.toLowerCase())))
return { data: matches.slice((page - 1) * pageSize, page * pageSize), meta: { page, pageSize, total: matches.length}}
}
return result
},
article(id: string): Promise<KnowledgeArticle> {
const mock = mockArticles.find((row) => row.id === id)
return ENABLE_HOME_MOCK && mock? Promise.resolve(mock): api.knowledgeArticle(id)
},
related(id: string): Promise<KnowledgeArticle[]> {
return ENABLE_HOME_MOCK && id.startsWith('mock-')? Promise.resolve(mockArticles.filter((row) => row.id!== id)): api.knowledgeRelated(id)
},
}
