# AI辅助评估服务端与验收（2026-10-10）

## 交付范围

本轮按用户新确认纳入AI服务端开发，替代此前“两端独立缓存演示”的运行方式。患者和后台读取同一MySQL任务；原演示工具文件保留，但业务页面不再导入或依赖其数据。

已完成：独立任务、AI用途授权及正文快照、三角度私有照片、异步质检、不可变输入快照、持久队列、租约与重启恢复、有限重试、严格结果校验、同院区在岗医生分配、复核与更正、患者结果投影、追加审计，以及照片清理/加密备份/恢复引用。

当前本地服务未配置真实质检/模型网关。照片保存后明确显示“检查暂不可用”；提交返回503 `MODEL_CONFIGURATION_ERROR`，不回退到演示结果、不把未知检查判为通过。模型与质检工程验收采用合成图片和受控替身；尚未验证真实人脸、角度判定、医学效果或供应商服务。

## 数据与接口

新增迁移74：`assessments`、`assessment_photos`、`assessment_input_snapshots`、`assessment_attempts`、`assessment_results`、`assessment_reviews`、`assessment_consent_events`、`assessment_events`、`assessment_operations`。开发库及专用测试库已迁移至74。

接口前缀 `/api/v1`，详情成功返回 `{data:{task,photos,consent,publishedReview,...}}`；患者响应不包含原始结果、内部员工ID、供应商请求号或尝试记录。

| 接口 | 行为 |
| --- | --- |
| GET `/assessment-capabilities` | 当前配置能力及尝试上限；不返回地址/密钥 |
| GET/POST `/me/assessments` | 本人分页任务；独立创建，无需预约 |
| GET `/me/assessments/{id}` | 本人任务及医生发布内容 |
| GET/POST `/me/assessments/{id}/consent` | 查询、明确同意或撤回独立 `photo-ai-v1` 授权 |
| PUT/POST `/me/assessments/{id}/photos/{angle}` | 原生小程序使用POST multipart；后台接口兼容PUT |
| POST `/me/assessments/{id}/quality-check` | 重检查入队，202，不等待检查服务 |
| POST `/me/assessments/{id}/submit`、`retry`、`cancel` | 提交/重试202；取消200 |
| GET `/me/assessments/{id}/photos/{photoId}/content` | 本人私有读取，包括撤权后的本人历史 |
| GET `/admin/assessments`、`/{id}` | 按院区和分配关系的队列/详情 |
| GET `/admin/assessments/reviewers?clinicId=...` | 同院区启用、在岗医生选项 |
| POST `/admin/assessments/{id}/assignment` | 主管分配/改派，校验当前版本 |
| POST `/admin/assessments/{id}/reviews` | 当前分配医生复核/更正；主管不能代审 |
| GET `/admin/assessments/{id}/events` | 有权人员读取审计 |
| GET `/admin/assessments/{id}/photos/{photoId}/content` | 读取前后重新核验员工与AI授权 |

除创建正文自带`requestKey`外，写操作须带`Idempotency-Key`；正文带任务`version`。同键同内容恢复原响应，同键异内容409。创建201，同键重放200。图片multipart仅接受`file`和单个`version`，拒绝未知或重复字段。

图片以实际解码校验JPG/PNG、8MiB、短边512及最长边4096；处理8种EXIF方向、移除元数据并将透明背景归一为白色JPEG。每角度仅一个当前引用，更换图使旧检查失效，重复规范化哈希阻断。提交冻结照片、质量版本、模型配置和授权事件；提交后替换409。冻结引用不被孤立清理删除。

## 授权、执行与复核

- 当前隐私同意和AI授权均为提交/上传/重试/外呼前提。诊疗授权或案例授权不能代替AI授权。检查当前AI授权事件的版本和正文；过时授权不扩大用途，旧客户端仍可撤回。
- 所有任务修改按患者行、任务行顺序加锁，队列每患者最多一项活动任务；创建限5秒一次、每小时10项，同键恢复不消耗新建次数。改派及复核额外锁定在岗人员与员工，避免资格校验后发生权限变更。
- 质检是持久请求，故障不消耗模型尝试。必检7项：编码、尺寸、人脸、清晰度、曝光、遮挡、角度；未知或失败不能提交。版本不匹配、缺字段、错误结构均不可判通过。未检查时`checkedAt=null`。
- 分析租约90秒，每20秒续租；外呼超时60秒。自动/手动共用3次上限，10/30秒退避，并尊重较长Retry-After（上限24小时）。进程恢复处理过期租约，旧尝试不能覆盖结果。
- 供应商请求键绑定冻结输入快照。结果落库失败可在租约内重试持久化，不再调用模型。原始有效结果至多一份，复核及更正追加版本。
- 模型响应校验版本、标签、长度、完整必填字段、证据归属、观察ID唯一、有限数值、未知字段、重复JSON键和尾随内容。`requiresHumanReview`固定true；`inconclusive`不含观察，不伪造正常结论。
- 仅当前分配、在岗、启用且具备院区权限的doctor可复核。护理、客服、运营及兼容clinician角色不能进入；主管可查看及分配，不能审批。
- 撤回取消任务、结束活动尝试、禁止新调用与医护内容读取，迟到回包不落库；重新同意不能复活旧任务。本人历史及审计保留。恢复文件不恢复授权。

患者页面每5秒按服务端状态刷新，隐藏停止并清除内容；创建丢失响应保留原申请，进度页原操作可跨刷新恢复，上传中断保留原照片与请求号。后台保存结果不明时保留原请求，版本冲突保留编辑内容；私有预览沿现有10秒重鉴权、15秒到期清除机制。

## 质检/模型网关配置

使用项目定义的`assessment-gateway-v1`协议。此适配器可调用已配置的受控网关；不是某一厂商SDK，也不表示已部署真实模型/检查器。真实服务应提供以下两个端点，并在网关内适配实际检查器及供应商。调用方必须冻结并遵守请求中的模型、提示词和检查配置，不能把旧任务默默升级到新模型。

配置项见[环境变量示例](../.env.example)：

```dotenv
ASSESSMENT_GATEWAY_URL=https://<受控网关>/assessment/v1
ASSESSMENT_GATEWAY_KEY=<服务端密钥>
ASSESSMENT_CHECKER_VERSION=<检查器版本>
ASSESSMENT_THRESHOLD_PROFILE_VERSION=<阈值配置版本>
ASSESSMENT_MODEL_PROVIDER=<服务提供方>
ASSESSMENT_MODEL_ID=<模型编号>
ASSESSMENT_MODEL_VERSION=<模型版本>
ASSESSMENT_DEPLOYMENT_VERSION=<部署版本>
ASSESSMENT_PROMPT_VERSION=<提示词版本>
ASSESSMENT_ADAPTER_VERSION=<适配器版本>
```

全空允许本地服务运行并显示未配置；部分配置拒绝启动。远程要求HTTPS和密钥，只有loopback测试允许HTTP；拒绝URL内账号、查询串、片段及跳转，不把密钥存入快照或前端。配置更改后重启API。

两个POST请求均带服务端Bearer密钥，`/analyze`另带绑定输入快照的`Idempotency-Key`。请求结构：

```json
{
  "schemaVersion": "assessment-gateway-v1",
  "configuration": {
    "model": {"provider":"...","modelId":"...","modelVersion":"...","deploymentVersion":"...","promptVersion":"...","adapterVersion":"..."},
    "checkerVersion": "...", "thresholdProfileVersion": "..."
  },
  "photos": [{"id":"photo-...","angle":"front","sha256":"...","width":512,"height":512,"jpegBase64":"..."}]
}
```

| 端点 | 成功响应 |
| --- | --- |
| POST `{ASSESSMENT_GATEWAY_URL}/quality` | `{ "qualities": { "photo-id": <AssessmentQuality> } }`，每张图一项，完整必检项及版本 |
| POST `{ASSESSMENT_GATEWAY_URL}/analyze` | `{ "analysis": <AssessmentAnalysis>, "providerRequestId": "..." }` |

完整结构以[OpenAPI](../packages/contracts/openapi.yaml)及[规格](AI辅助评估字段与验收.md)为准。响应上限1MiB，429/5xx/超时按受控错误处理；凭据/配置及非法结果不回退示例、不记录供应商原始错误正文。配置能力标志表示配置齐全，不代替供应商可用性或效果检查。

## 本轮验证

| 验证 | 结果与证据 |
| --- | --- |
| 契约、类型、小程序、双端构建 | `pnpm check`通过；小程序128项，包括新的AI页面7项。[日志](../tmp/assessment-development-20261010/pnpm-check.log) |
| Go与真实MySQL全量 | 139个顶层、288个子测试通过；无失败/跳过。[JSONL](../tmp/assessment-development-20261010/go-test.jsonl) |
| Go静态检查 | `go vet ./...`通过。[日志](../tmp/assessment-development-20261010/go-vet.log) |
| 浏览器全量 | 80/80通过，无失败/跳过/不稳定项。[结果](../tmp/assessment-development-20261010/playwright-final.json) |
| AI浏览器专项 | 7/7通过：修订、分派、结果不明恢复、版本冲突、撤权/未配置、筛选及护理门禁。[结果](../tmp/assessment-development-20261010/playwright-ai-verified.json) |
| 真实运行服务AI | 6项通过：10并发创建、三图上传、未配置拒绝、双端共享/本人隔离、撤权访问、禁止复活。[结果](../tmp/assessment-development-20261010/live-ai.json)及[脚本](../tmp/assessment-development-20261010/live-ai.mjs) |
| 提交响应 | 本轮首次提交12.3629ms，20次原请求/重放样本p95 9.3769ms，未等待模型；不是20个独立患者的性能压测。[测试](../services/api/internal/store/assessment_http_integration_test.go) |

A01–A20工程基线对应测试：创建/冻结/授权/投影见`assessment_integration_test.go`；质检失败、版本升级、院区、备份及隐私见`assessment_acceptance_test.go`；worker及复核竞争、落库失败、手动重试见`assessment_concurrency_test.go`；HTTP、规范化图像、权限与网关见同名前缀测试；页面刷新/身份/原请求恢复见小程序和浏览器专项。未做真实30秒模型延迟或真机验证，也未进行真实模型准确性/供应商集成验收。

复现：服务启动后执行`pwsh -File scripts/check.ps1`；`.env`须有专用`*_test`库的`TEST_DB_DSN`。真实AI脚本仅用于未配置网关的安全边界检查，测试只使用自生成几何JPEG，不使用患者照片。开发库保留演示任务与授权/撤回审计。

仍需外部条件：真实检查器和模型网关、效果样本及医生金标准、正式AI授权文本/提供方/保存与删除规则、微信登录与系统选图真机验收、正式云资源及上线配置。本轮工程通过不能替代这些验收。
