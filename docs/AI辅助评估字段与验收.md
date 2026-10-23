# AI 辅助评估：结果字段与验收标准

日期：2026-10-03。版本：`assessment-spec-v1`。状态：规格已整理；2026-10-08 已完成四类页面及本地演示流程，见《AI辅助评估页面与演示验收》。2026-10-10已实现真实任务持久化、双端接口与网关适配，见[服务端交付](AI辅助评估服务端与验收.md)；真实质检/供应商及模型效果尚未验收。本文是实施基线；工程默认参数可配置，质检阈值及真实模型效果门槛须在正式接入前冻结。

## 1. 范围与现有流程

患者登录后选择院区，独立创建评估，不要求已有预约；可选关联本人同院区预约。任务包含正面、左侧、右侧照片、质量检查、异步分析、失败重试、结果存储、模型版本和医生复核。评估未完成或失败不阻断预约。

现有 `care_photos` 绑定预约，随访照片绑定随访任务，新增独立评估照片归属并复用私有存储基础能力。后续若从已有照片导入，必须验证本人归属、AI 用途授权并生成不可变输入快照，不能绑定“当前最新照片”。媒体清理、备份和恢复必须纳入评估照片引用。

首版输出照片可观察现象、观察局限和经医生复核的面诊建议。原始 AI 结果只在医护端展示，患者在复核前仅见“分析完成，待医生复核”。不自动输出正式诊断、处方、治疗参数、价格或疗效承诺。这是本模块拟定的产品规则。

## 2. 独立任务及三角度照片

ID 为不透明字符串；时间为服务端 RFC 3339，界面按上海时区显示。未发生的时间及对象返回 `null`，集合返回 `[]`。身份、操作者和时间不从请求正文采信。

| 任务字段 | 类型 / 约束 | 说明 |
| --- | --- | --- |
| `id` / `patientId` | string，必填 | 任务及患者归属，患者从会话确定 |
| `clinicId` | string，必填 | 创建时验证启用且公开的院区；提交后冻结 |
| `appointmentId` | string或null | 可选，必须为本人同院区预约 |
| `requestKey` | string，必填 | 创建幂等键，患者作用域唯一；同键异内容409 |
| `version` | integer ≥ 1 | 上传替换、提交、重试、分配和复核的乐观锁 |
| `status` | 状态枚举 | 仅服务端可更新，见状态机 |
| `consentEventId` / `consentVersion` | string或null | 独立AI用途授权及版本，提交时留快照 |
| `inputSnapshotId` | string或null | 提交冻结的三角度输入 |
| `reviewerEmployeeId` | string或null | 主管分配的复核医生 |
| `attemptCount` / `maxAttempts` | integer；默认上限3 | 已开始尝试数及本次提交的总尝试上限 |
| `nextAttemptAt` | datetime或null | 下次执行时间 |
| `failureCode` / `retryable` | string或null / boolean | 受控失败类型及当前能否重试 |
| `resultId` / `publishedReviewId` | string或null | 有效分析结果及患者可见复核版本 |
| `createdAt` / `updatedAt` | datetime | 创建/变更时间 |
| `submittedAt` / `analysisCompletedAt` / `reviewedAt` | datetime或null | 业务节点时间 |
| `supersedesAssessmentId` | string或null | 补拍或升级重跑时关联本人同院区旧任务 |

照片元数据：`id`、`assessmentId`、`angle: front|left|right`、`mediaType`、`sizeBytes`、`width`、`height`、`sha256`、`uploadedAt`、`qualityCheck`。私有对象键只存服务端，不返回裸图片地址。哈希基于服务端规范化后的字节。

每角度仅一个草稿当前引用。替换增加任务版本，旧质检失效。提交后冻结院区、照片、输入哈希、质检版本及模型配置，补拍创建关联新任务；旧照片不能被媒体清理误删。三张相同规范化哈希的重复图阻止提交；手选角度不等于已验证照片角度或同一身份。

## 3. 图片质量检查字段

客户端检查用于提示，服务端决定是否通过。沿用 JPG/PNG、最大8MiB、最大边长4096像素，新增最短边512像素的工程候选门槛。验证真实编码、解码像素上限和完整解码，规范方向并移除EXIF，不信任扩展名/MIME。

| `qualityCheck` 字段 | 类型 / 枚举 | 规则 |
| --- | --- | --- |
| `status` | `pending|passed|rejected|unavailable` | unavailable是检查服务故障，不表示图不合格 |
| `checkerVersion` / `thresholdProfileVersion` | string | 检查器和阈值配置版本；无版本不能通过 |
| `checkedAt` | datetime或null | 检查完成时间 |
| `checks` | 数组 | 每项 `code`、`status: pass|fail|unknown`、`score: number|null`、`threshold: number|null`、`message` |
| `issues` / `retakeAdvice` | string[] | 问题代码及按角度给出的补拍提示 |

必检项为合法编码/尺寸、恰好一张可用人脸、清晰度、曝光、遮挡、声明角度匹配及跨照片重复。模糊、曝光、遮挡与角度评分范围/阈值由选定检查器定义，不能混用不同算法数值。配置须列出各项阈值、版本及样本验证；任一必检失败或unknown均不能入队。缺少检查器返回unavailable，不能默认passed。

提交事务再次核验三张当前照片、通过的检查及有效授权，再冻结输入并入队。质检服务暂不可用时可重检查草稿，不消耗模型尝试数；质检重试需限流。更换图或阈值后重检，不覆盖历史检查快照。

## 4. 分析结果字段

结果schema为 `assessment-result-v1`，与任务分表存储，原始结果不可覆盖。每个结果绑定任务、冻结输入与有效尝试。

| 字段 | 类型 / 约束 | 说明 |
| --- | --- | --- |
| `id` / `assessmentId` / `attemptId` / `inputSnapshotId` | string，必填 | 完整追溯链 |
| `schemaVersion` | 固定 `assessment-result-v1` | 字段和枚举版本 |
| `generatedAt` | datetime | 服务端校验并存储结果的时间 |
| `model` | 对象，必填 | 非空 `provider`、`modelId`、`modelVersion`、`deploymentVersion`、`promptVersion`、`adapterVersion` |
| `outcome` | `observations_available|inconclusive` | 有可观察现象 / 不能形成可靠观察 |
| `summary` | string，1–1000字符 | 可见现象概要 |
| `observations` | 数组，0–30项 | 观察项目，见下表 |
| `limitations` | string[]，1–20项，每项1–500字符 | 照片、光照或模型局限，至少一项 |
| `consultationAdvice` | 对象 | `recommended: boolean`、`reason: string`（1–1000字符）、`preparation: string[]`（0–10项，每项最多500字符） |
| `requiresHumanReview` | 固定true | 服务端固定，不接受模型关闭 |

| 每项 `observations` 字段 | 类型 / 约束 | 说明 |
| --- | --- | --- |
| `id` | string，结果内唯一 | 复核定位具体观察 |
| `code` | `surface_irregularity|color_variation|other_visible_change` | 工程观察分类，不自动映射医学痘坑类型/分级 |
| `description` | string，1–500字符 | 照片中可见变化 |
| `region` | `forehead|left_cheek|right_cheek|nose|chin|multiple|unknown` | 左右按患者自身方位 |
| `evidence` | 数组，1–3项 | 每项 `photoId`、`angle`，仅引用冻结输入且角度一致 |
| `certainty` | `low|medium|high|unknown` | 模型自报确定程度，不能解释为正确概率 |

首版不加入痘坑确诊、严重程度分数、精确数量/面积、治疗方案/参数及自动急症分流。增加此类字段需独立标签定义、标注标准和效果验证，并升级schema。

服务端严格校验必填、长度、上限、枚举、唯一观察ID及证据归属，拒绝未知字段。inconclusive要求observations为空且说明原因。非法JSON、缺少版本、未知标签或越界证据不能标成功。文本按纯文本渲染，不执行模型HTML、链接或指令；结构校验不替代医生内容复核。

不能只记录可变模型别名。供应商不披露修订号时，modelVersion记录明确的“未披露”标识并冻结服务器deploymentVersion，不能编造版本；这类配置不能通过精确模型版本可追溯的正式验收。新模型配置只影响新提交，原任务重试沿用原配置；升级重跑创建关联新任务。

## 5. 人工复核与患者结果

复核字段：`id`、`assessmentId`、`resultId`、`version`、`decision: approved|amended|rejected`、`note`（1–2000字符）、`reviewedByEmployeeId`、`reviewedAt`、`finalContent`、`supersedesReviewId`。

通过/修订保存完整finalContent快照（summary、observations、limitations、consultationAdvice），沿用结果字段结构约束；通过复制原内容，修订必须说明更正依据。拒绝时finalContent=null，填写面向患者的补拍/面诊说明。原AI结果保留不变。复核与发布在同一事务完成。

仅当前分配、在岗、账号启用、角色doctor且具备任务院区授权的医生可复核。主管可在授权院区查看/分配/改派和看审计，manager身份不能直接审批；护理、客服、运营及旧clinician角色无本模块复核权限。任务权限由评估负责人关系决定，不能依赖预约归属。改派增加任务版本，离岗/旧负责人无法提交旧页面。

患者DTO仅返回本人任务、复核后的finalContent、医生公开姓名、复核时间/版本及“照片辅助观察，具体以面诊为准”说明，不返回原AI观察、供应商原始响应、提示词、内部员工ID和失败堆栈。未复核只显示进度；拒绝显示说明及补拍/预约入口；inconclusive不伪造正常结论。

发布后的更正追加复核版本，并更新当前发布指针，旧复核不可覆盖；患者刷新读取最新快照。所有复核与更正均要求任务版本、结果ID和幂等键。

## 6. 状态机、异步与重试

| 状态 | 含义 | 合法后续 |
| --- | --- | --- |
| `draft` | 可上传/替换、质检 | queued、cancelled |
| `queued` | 输入和配置已冻结 | analyzing、cancelled |
| `analyzing` | worker持有效租约执行 | awaiting_review、retry_wait、failed、cancelled |
| `retry_wait` | 可重试且未达上限 | queued、cancelled |
| `failed` | 不可重试或用尽次数 | queued（满足手动重试条件）、cancelled |
| `awaiting_review` | 合法结果已原子保存 | completed、rejected、cancelled |
| `completed` | 医生通过/修订并发布 | 同状态追加更正；撤回授权转cancelled |
| `rejected` | 医生拒绝并保存说明 | 补拍建新任务；撤回授权转cancelled |
| `cancelled` | 用户取消或撤回AI授权 | 无；再次同意不复活任务 |

MySQL保存队列与尝试，业务状态和入队在同一事务提交。提交HTTP返回202与任务ID，不等待模型调用。worker至少一次执行，业务有效结果至多一次生效。

尝试记录：`id`、`attemptNo`、冻结配置、`startedAt`、`finishedAt`、`leaseExpiresAt`、`leaseToken`、`providerRequestId`、`errorCode`、`retryable`。领取用行锁或条件更新；首版工程默认租约90秒、每20秒续租、调用超时60秒。仅当前租约/尝试可落库，迟到/重复响应不能覆盖状态。重启扫描过期租约，将原尝试结束为可重试故障并计入上限。

一次提交最多3次模型尝试，失败后退避10秒、30秒，供应商Retry-After更长时按其等待。自动/手动共用上限；手动仅允许failed、可重试且attemptCount<maxAttempts。用尽次数须创建关联新任务，不能连点重试绕过上限。每患者最多一个queued/analyzing/retry_wait任务，在患者锁下检查；另设入口限流防止重复新建绕过限制。

| 失败代码 | 是否可重试 | 行为 |
| --- | --- | --- |
| MODEL_TIMEOUT / MODEL_RATE_LIMITED / MODEL_UNAVAILABLE / WORKER_LEASE_EXPIRED | 未达上限允许 | 退避并记录每次尝试 |
| MEDIA_UNAVAILABLE | 未达上限允许 | 不能缺图分析 |
| MODEL_OUTPUT_INVALID | 不允许原任务重试 | 修复配置/适配器后建关联新任务 |
| MODEL_CONFIGURATION_ERROR | 不允许 | 配置/凭据故障，不回退演示结果 |
| INPUT_QUALITY_REJECTED | 不允许分析 | 草稿补拍，冻结后建新任务 |
| AI_CONSENT_REVOKED | 不允许 | 取消并阻止后续调用、写入和医护读取 |

结果落库失败时不确认成功；在租约有效期内重试数据库事务，租约失效拒绝写入。结果唯一约束防重复提交；供应商支持时传稳定请求键。业务幂等不能保证供应商调用/计费一次，重复调用需可审计。

## 7. 授权、存储和访问

新增独立photo-ai-v1授权，说明三角度照片、AI分析、医生复核和供应商处理用途；隐私同意和photo-care-v2不能代替它。创建草稿可先不授权，上传/提交/重试/调用要求当前隐私同意及有效AI授权；正文、版本、事件及时间追加保存，不默认勾选。

撤回不受隐私提交门禁阻断：取消任务、停止医护照片/结果访问及后续调用。调用前/结果提交前再核验授权和任务版本，迟到结果拒绝写入。已发给供应商的数据不能因取消立即收回，保留与删除机制须正式接入前明确。再次同意须新建任务；本人仍可看本人既有历史及撤回状态，人工发布记录保留审计，不因撤回覆盖。

建议表：assessments、assessment_photos、assessment_input_snapshots、assessment_attempts、assessment_results、assessment_reviews、assessment_consent_events、assessment_events。唯一约束至少包括患者+创建键、任务+当前角度、任务+尝试号、有效结果与尝试、任务+复核版本。冻结照片引用纳入清理/备份/恢复；孤立对象按既有规则回收。

服务端统一检查本人/院区/负责人及有效授权，列表计数同样隔离；媒体和结果响应no-store。退出/切换身份清理预览缓存，后台沿现有周期重新鉴权照片。原始响应如确需排错须受控保存，不写普通日志。保存期限、供应商地域与删除能力是正式接入待确认项。

审计追加记录创建、照片替换、质检、提交、领取、失败、重试、落库、分配/改派、复核/更正、取消、授权撤回及医护读取。含操作者、服务端时间、状态/版本和结果/尝试引用，普通日志不含原图、base64、密钥、姓名手机号或签名地址。

## 8. 拟定接口与页面

以下接口已加入OpenAPI并生成Go/TypeScript类型，具体兼容方式及响应见服务端交付记录。

| 接口（前缀 `/api/v1`） | 行为 |
| --- | --- |
| POST `/me/assessments` | 创建草稿，201；同键重放恢复同任务 |
| GET `/me/assessments`、`/me/assessments/{id}` | 本人分页列表/详情、质量提示、复核后内容 |
| GET/POST `/me/assessments/{id}/consent` | 查看/明确同意/撤回，检查协议版本 |
| PUT `/me/assessments/{id}/photos/{angle}` | 携任务版本上传/替换草稿图 |
| GET `/me/assessments/{id}/photos/{photoId}/content` | 本人私有读取 |
| POST `/me/assessments/{id}/quality-check` | 草稿质检故障后重检查 |
| POST `/me/assessments/{id}/submit`、`.../retry`、`.../cancel` | 携版本和操作键；提交/重试202，取消200 |
| GET `/admin/assessments`、`/admin/assessments/{id}` | 授权主管/分配医生内部详情及分页队列 |
| GET `/admin/assessments/{id}/photos/{photoId}/content` | 有当前授权及院区/负责人权限的读取 |
| POST `/admin/assessments/{id}/assignment` | 主管分配同院区启用、在岗医生 |
| POST `/admin/assessments/{id}/reviews` | 医生复核/更正，携版本、结果ID及操作键 |
| GET `/admin/assessments/{id}/events` | 有权查看任务者读取受控审计 |

状态/幂等冲突409、用途未授权403、越权资源404、非法请求400、文件过大413、质检不合格提交422、服务未配置/不可用503。每次写入使用独立操作幂等键，相同键相同内容返回原操作结果，不再增加尝试/复核；响应丢失时以原键/任务查询恢复。

小程序：评估列表、院区选择/独立创建、三角度上传、进度/补拍/失败、复核结果与预约入口。后台：队列/分配、照片对照、原始结果/模型版本、复核、更正历史。页面可见时每5秒轮询，隐藏停止，返回按任务ID恢复；不能由本地缓存推断成功。模型未配置明确显示不可用。

## 9. 工程验收标准

以下工程基线已补充自动化回归；对应覆盖与实测范围见服务端交付记录。使用独立_test数据库、合成照片和可控模型替身；真实模型另验收。

| 编号 | 场景 | 通过标准 |
| --- | --- | --- |
| A01 | 无预约创建、10并发相同创建、同键异内容 | 可独立创建；仅一任务；异内容409 |
| A02 | 三角度上传/替换及提交后替换 | 每角度一个当前引用；替换质检失效；冻结后409且哈希不变 |
| A03 | 缺图、重复图、他人图、跨院区关联 | 不入队、不外呼；越权不泄露资源 |
| A04 | 伪MIME、损坏图、超8MiB、最大边4097/最短边511 | 服务端拒绝且无有效引用；合规尺寸进入质检；EXIF移除 |
| A05 | 模糊/曝光/遮挡/人脸/角度失败及质检故障 | fail/unknown阻止提交；逐角度提示；故障不默认passed；版本可追溯 |
| A06 | 模型等待30秒、提交响应丢失后重放 | 健康本地环境202响应p95≤1秒（不含上传）；仅一快照/队列，不等模型 |
| A07 | 双worker竞争、重复执行、过期worker迟到 | 仅一有效租约及有效结果；旧租约不可写入/覆盖 |
| A08 | 超时、429/5xx、进程重启 | 10/30秒和Retry-After退避；最多3次；重启补处理租约 |
| A09 | 手动连点/自动重试竞态、次数用尽 | 仅一次入队；共用3次上限；不可重试无重试入口 |
| A10 | 非法JSON、未知字段/枚举、超长文本、越界证据 | 不保存成功结果、不展示观察；记录MODEL_OUTPUT_INVALID |
| A11 | 合法结果、inconclusive、重启读取 | 结果/状态原子保存；追溯输入/尝试/模型；无法判断不显示正常结论 |
| A12 | 模型A升级B、A任务重试、复核更正 | 老任务沿用A、新任务用B；AI原文不变；历史可追溯 |
| A13 | 未复核、通过、修订、拒绝、并发复核 | 未复核患者无原AI；通过/修订仅finalContent；拒绝有说明；同版本一次成功 |
| A14 | 跨患者/院区、未分配、护理/客服/运营、离岗 | 后端拒绝敏感读取/复核；主管不越区分配、不凭角色审批；列表计数不泄露 |
| A15 | 无AI同意、仅诊疗授权、协议版本变化 | 禁止上传/提交/调用；需明确同意新版本；不阻断预约/撤回 |
| A16 | 排队/调用中/复核后撤回、迟到返回、再次同意 | 取消并阻止后续调用/医护读取/迟到落库；旧任务不复活 |
| A17 | 无模型配置、凭据失效、媒体故障、落库失败 | 明确错误；无演示回退/缺图分析；无重复有效结果；日志无敏感信息 |
| A18 | 清理/备份/恢复评估图、恢复后撤回核验 | 冻结引用不误删；照片可核验恢复；不复活旧授权/公开状态 |
| A19 | 刷新/返回、隐藏、退出/换身份、打开预览后撤回 | 按服务端恢复；隐藏停轮询；旧缓存清除；照片按既有周期重鉴权 |
| A20 | 预约/诊疗照片/方案/随访/统一档案回归 | 不混用用途授权、不放宽原权限；任意评估状态都可正常预约 |

## 10. 模型效果与实施次序

工程流程、真实供应商集成、模型效果分别记录。替身和合成结果不能证明观察准确性。正式报告应记录授权独立验证集来源/规模、设备/光照/肤色/角度分组、医生标注和分歧处理、质检误接收/误拒绝率、观察标签准确率/召回率、证据匹配率、无法判断率、医生修订/拒绝率、禁止内容发生率、延迟/成本。验证集不能用于调参训练。

当前尚未指定模型、检查器或医生确认的金标准，不编造准确率目标。正式接入前冻结模型版本、每项阈值、样本量、指标目标及置信区间口径，并出具独立验证报告。A01–A20全通过、真实集成与权限/撤回通过、精确版本可追溯且满足冻结的效果门槛，才可标为正式验收完成。

实施顺序：契约/迁移 → 独立任务/授权/照片质检 → 持久队列/worker/模型适配器/结果校验 → 分配/复核及患者投影 → 双端页面 → 故障与集成验收。缺少正式模型时可用显式开发替身验证，必须标为开发数据，不向真实患者开放。

待正式接入确认：供应商/精确模型版本、质检算法与阈值、验证集及效果目标、复核医生与时限、授权正文、数据保留/删除、调用预算及每患者限流。可先实现本地流程，但不能据此宣称真实模型或生产验收完成。
