import { api, type KnowledgeArticle, type KnowledgeCategory } from './api';
import { ENABLE_HOME_MOCK } from '../config/presentation';

// Mock only fills a successful empty response for the existing demo clinic.
// Network failures remain visible. Formal published articles always take priority.
const mockCategory: KnowledgeCategory = { id: 'mock-visit', clinicId: 'clinic-demo', name: '就诊准备（演示）', sortOrder: 0, active: true, version: 1 };
const mockArticles: KnowledgeArticle[] = [
  { id: 'mock-knowledge-1', categoryId: mockCategory.id, title: '面诊前，如何整理沟通问题（演示）', summary: '科普栏目样例：记录关注的问题、既往就诊和用药信息。',
    body: '【Mock 内容，仅用于栏目功能演示】\n\n可以将希望与医生沟通的问题整理成清单，并准备既往就诊、治疗及用药信息。\n\n到院前查看预约确认结果和时间；具体诊疗建议以医生面诊为准。', sortOrder: 0, reviewerName: '演示审核人（虚构）', reviewedAt: '2026-10-01T09:00:00+08:00', publishedAt: '2026-10-01T10:00:00+08:00' },
  { id: 'mock-knowledge-2', categoryId: mockCategory.id, title: '认识面诊、方案与随访流程（演示）', summary: '科普栏目样例：了解从预约到查看个人方案、填写随访反馈的流程。',
    body: '【Mock 内容，仅用于栏目功能演示】\n\n提交面诊预约后，等待工作人员确认。面诊后，可在预约详情查看已发布的个人方案。\n\n治疗后的随访安排可在恢复日历中查看。本文是产品流程样例，不提供具体治疗或用药建议。', sortOrder: 1, reviewerName: '演示审核人（虚构）', reviewedAt: '2026-10-01T09:00:00+08:00', publishedAt: '2026-10-01T10:00:00+08:00' },
];
export const knowledge = {
  async categories(clinicId: string, allowMock = true) {
    const rows = await api.knowledgeCategories(clinicId);
    // Only display a mock category when the public article list is also empty.
    if (!ENABLE_HOME_MOCK || !allowMock || clinicId !== 'clinic-demo' || rows.length) return rows;
    const articles = await api.knowledgeArticles(clinicId, '', 1, 1);
    return articles.meta?.total || articles.data?.length ? rows : [mockCategory];
  },
  async articles(clinicId: string, categoryId = '', page = 1, pageSize = 20, allowMock = true, q = '') {
    const result = await api.knowledgeArticles(clinicId, categoryId.startsWith('mock-') ? '' : categoryId, page, pageSize, q);
    if (ENABLE_HOME_MOCK && allowMock && clinicId === 'clinic-demo' && (!categoryId || categoryId === mockCategory.id) && (result.meta?.total ?? result.data?.length ?? 0) === 0) {
      if (q) { const all = await api.knowledgeArticles(clinicId, '', 1, 1); if (all.meta?.total || all.data?.length) return result; }
      const matches = mockArticles.filter(row => !q || [row.title, row.summary, row.body].some(text => text?.toLowerCase().includes(q.toLowerCase())));
      return { data: matches.slice((page - 1) * pageSize, page * pageSize), meta: { page, pageSize, total: matches.length } };
    }
    return result;
  },
  article(id: string): Promise<KnowledgeArticle> {
    const mock = mockArticles.find(row => row.id === id);
    return ENABLE_HOME_MOCK && mock ? Promise.resolve(mock) : api.knowledgeArticle(id);
  },
  related(id: string): Promise<KnowledgeArticle[]> { return ENABLE_HOME_MOCK && id.startsWith('mock-') ? Promise.resolve(mockArticles.filter(row => row.id !== id)) : api.knowledgeRelated(id); },
};
