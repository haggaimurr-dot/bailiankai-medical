# 本地开发 API（M0 + M1 + M2）

Go 1.27.1、Gin、GORM、Goose 与 MySQL 8.4。Go 工具链由 `go.mod` 固定；首次运行会自动下载。所有医生、院区均为虚构演示资料。

## 启动

在本目录运行。先按照仓库根目录说明启动 MySQL，并将 `.env.example` 对应配置导入当前进程环境；程序不会自动读取 `.env`。

```powershell
go run ./cmd/migrate
go run ./cmd/seed
go run ./cmd/bootstrap-employee
go run ./cmd/api
```

- 必须设置 `APP_ENV=development`、`ENABLE_DEMO=true` 和 `DB_DSN`。首次运行 `bootstrap-employee` 还须设置 `EMPLOYEE_CREDENTIALS_FILE`，用于保存随机生成的主管初始密码；根目录启动脚本会自动处理。
- 默认监听 `127.0.0.1:8080`，`HTTP_ADDR` 只接受 localhost/loopback 地址。
- `go run ./cmd/migrate` 执行嵌入的 Goose 向上迁移；没有 GORM AutoMigrate。
- `go run ./cmd/seed` 插入一家演示院区、两名演示医生，以及上海时区今天尚未开始的时段和后续六天时段。重复执行不会覆盖后台修改。每个时段 30 分钟，默认容量 3。
- 服务不会自行迁移或重建数据库。`/healthz` 检查进程；`/readyz` 检查数据库连接，以及排班、患者、会话、预约和审计表的必要字段。

## 接口

案例库需迁移24：`/api/v1/admin/cases` 整理审核及发布撤回，`/api/v1/me/cases` 本人查询和独立授权，`/api/v1/cases` 公开查询。私有副本纳入图片引用目录；诊疗照片授权不等于展示授权。见 [案例库接口与验收](../../docs/案例库接口与验收.md)。

唯一接口契约位于 `../../packages/contracts/openapi.yaml`。生成 Go 类型：

内容与知识库需迁移22、51、62：公开查询 `/api/v1/knowledge/categories`、`/articles`、`/articles/{resourceId}`；后台 `/api/v1/admin/knowledge` 提供分类、文章、医生审核、发布/下架、排序与历史。主管维护和发布，在岗医生审核，院区权限隔离；双端页面已接入，正式正文与参考来源由院方录入。详见 [内容与知识库接口](../../docs/内容与知识库接口.md)。

```powershell
go generate ./internal/apitypes
```

读取接口为 `/api/v1/clinics`、`/api/v1/doctors?clinicId=clinic-demo`、`/api/v1/slots?clinicId=clinic-demo&doctorId=doctor-demo-1&date=YYYY-MM-DD`。

查询条件可以省略；排班仅返回尚未开始的时段（包括关闭的时段），按开始时间和医生排序，最多 500 条。日期按上海时区解析。

修改接口为 `PATCH /api/v1/admin/slots/{slotId}`，需主管员工会话 `Authorization: Bearer <员工会话令牌>`，JSON 为 `{"capacity":3,"status":"open","version":1}`。容量范围 0–100，状态为 `open` 或 `closed`。保存后版本递增；版本冲突、容量小于已占用数量、已开始的时段均返回 409。

创建与批量生成使用 `POST /api/v1/admin/slots`，停诊使用 `POST /api/v1/admin/slots/suspend`，均要求主管员工会话及院区权限。需执行数据库迁移20。创建冲突整批回滚，停诊保留已有预约与名额并记录原因；操作规则与待确认业务扩展见 [排班管理规则与验收](../../docs/排班管理规则与验收.md)。

员工通过 `POST /api/v1/admin/session` 登录；令牌仅返回一次，数据库仅存摘要，12 小时过期。主管可开通、停用账号和重置密码；医护账号必须关联随访人员。改派、离岗交接、排班及预约审批由主管操作，医护可使用临床工作台。离岗人员的关联账号将无法访问后台。

医护账号的预约列表与详情仅限本人接诊的预约或本人当前负责随访的关联预约；随访队列、任务回评、任务历史及随访照片仅限当前负责人。治疗录入和方案编辑仅允许接诊医生，护理人员对关联预约资料只读。主管可查看全部资料。直接请求无权访问的详情或照片返回 404。

## M1 预约流程

### 演示患者身份

`POST /api/v1/dev/patient-sessions`，请求体必须为 `{}`，返回独立患者、随机会话令牌和到期时间。每次调用都会建立新的演示身份；不能提交 `patientId` 切换身份。数据库仅存令牌的 SHA-256 摘要，会话 7 天到期。`GET /api/v1/me` 使用患者 Bearer 令牌读取当前身份。患者令牌和员工会话不能互用。

没有接入微信登录、真实 AppID 或真实患者资料。会话失效后重新建立的是另一个演示身份，不能用来恢复原身份的预约。

### 患者接口

- `POST /api/v1/me/appointments`：请求体为 `slotId`、`contactName`、`contactPhone` 和可选 `note`。姓名为 1–40 字，大陆手机号为 11 位，备注最多 300 字；首尾空白会被清理。
- 请求必须带 `Idempotency-Key`（16–80 位字母、数字、`_` 或 `-`）。相同患者、相同 key、相同规范化内容只创建一次，首次返回 201，重放返回 200；相同 key 改换内容返回 409。
- `GET /api/v1/me/appointments/by-request/{requestKey}`：网络中断后，用原 key 查询提交结果。
- `GET /api/v1/me/appointments/{appointmentId}` 与 `GET /api/v1/me/appointments?status=&page=&pageSize=`：只能读取当前患者本人的预约；越权详情返回 404。
- `POST /api/v1/me/appointments/{appointmentId}/cancel`：请求体为 `{"version":1}`，开始前可取消待确认或已确认预约。

### 后台接口

- `GET /api/v1/admin/appointments?status=&date=&doctorId=&page=&pageSize=` 与 `GET /api/v1/admin/appointments/{appointmentId}`：管理口令读取预约，日期按上海就诊日期筛选。
- `POST /api/v1/admin/appointments/{appointmentId}/review`：请求体为 `{"action":"confirm","version":1}` 或 `{"action":"reject","version":1,"reason":"拒绝原因"}`。拒绝必须填写 1–200 字的原因，确认时原因留空。
- 列表分页默认 20 条、最多 100 条，返回 `data` 和 `meta:{page,pageSize,total}`。预约详情返回当前调用者可执行的 `canCancel`、`canReview`。

### 占号、过期和并发

1. 提交成功后进入 `pending`，立即占用一个名额；同患者同一时段最多一个待确认或已确认预约。
2. 后台确认后进入 `confirmed`，不会再次占号。拒绝、取消、待确认过期各释放一次名额；开始后不能取消。
3. 待确认默认 120 分钟到期，且最晚不超过时段开始。`PENDING_CONFIRMATION_MINUTES` 可设置 1–1440 分钟，只影响新申请，已保存的截止时间保持不变。
4. 进程启动时及每 15 秒检查持久化截止时间；查询和操作也执行过期补偿。因此停止后重启能补偿遗漏的过期任务。关闭进程时 worker 随上下文取消。
5. 状态变更使用患者 → 时段 → 预约的锁顺序，并在获取锁后重新计算时间，防止等待数据库锁后越过截止仍成功操作。预约状态、名额、时段版本和审计在同一事务更新。
6. 关闭时段只阻止新申请；既有申请继续遵循原来的审核和过期规则。

审计表只记录操作者、动作与状态变化，不复制姓名、手机号、备注或令牌。预约响应禁止缓存。此次交付仍限定本机开发环境，没有正式上线能力。

## M2 方案和照片演示

- `POST /api/v1/me/appointments/{appointmentId}/photo-consent`：患者为本人预约记录照片诊疗用途授权，或撤回授权。每次状态变化写入独立事件；版本为 `photo-care-v1`。
- `GET /api/v1/me/appointments/{appointmentId}/photos`、`PUT /api/v1/me/appointments/{appointmentId}/photos/{angle}`：读取和上传本人照片。角度为 `front`、`left`、`right`，每个角度每条预约保留一张，重传会替换当前照片。只接受最大 8 MB 的 JPG/PNG，服务端解码并重新编码为 JPEG，以去除原图元数据。
- `GET /api/v1/me/photos/{photoId}/content`：只有照片所属患者会话能读取原图。后台照片列表和内容接口要求当前有效的患者授权。
- `GET /api/v1/me/appointments/{appointmentId}/care-plan`：仅返回已发布方案版本，不会返回草稿。
- `/api/v1/admin/appointments/{appointmentId}/care-plan`：用本地演示后台口令读取工作区；`PUT .../draft` 按版本号保存草稿，`POST .../publish` 发布不可原地修改的新版本。
- 照片写入 API 进程工作目录对应的 `../../.local/private-media`，可用 `MEDIA_ROOT` 覆盖。目录不在 Web 静态资源路径下；照片必须通过鉴权接口读取。
- `MEDIA_DRIVER=cos` 可切换为腾讯云私有对象存储，要求桶权限检查通过且已启用版本控制；存储故障不会回退本地。API每小时清理超过24小时且数据库无引用的孤立对象；备份、恢复和本地迁移使用 `go run ./cmd/media`。配置与灾备步骤见 [图片私有存储与恢复](../../docs/图片私有存储与恢复.md)。当前尚未创建真实COS桶，云端演练待完成。
- 照片列表和原图读取、方案读取、草稿保存和发布会写入 `care_access_events`；照片上传和授权状态变化另有记录。后台照片读取每次都会再次检查患者当前授权。

预约照片、当前方案仅允许关联已存在的预约；方案草稿和发布要求预约已确认。方案编辑使用当前员工会话与职责权限。私有COS适配与运维工具已经提供，真实桶、院方授权文本及整套服务生产部署验收仍需完成后再用于真实服务。

## 验证

```powershell
go test ./...
go vet ./...
# 有 C 编译器时：
go test -race ./...
```

HTTP 单元测试覆盖鉴权、参数与时区、版本错误响应、空列表以及 CORS。真实 MySQL 集成测试要求显式设置 `TEST_DB_DSN`，且数据库名必须以 `_test` 结尾。测试会迁移并按外键顺序清空专用测试库内的 M1/M2 业务表。未设置该变量时测试会明确跳过，不能视作数据库验证通过。

集成验证包括：M0 排班版本冲突、seed 保留修改；患者会话摘要存储和过期、身份隔离；同 key 并发提交、不同 key 的重复活跃申请、多患者争抢最后名额；确认不重复占号、取消/拒绝/过期释放；重启后的过期补偿；取消、审核、过期的并发竞争；持锁跨过时段开始或确认截止后的拒绝；HTTP 提交、后台审核、患者读取结果，以及错误参数和分页。所有数据库集成测试放在同一个 Go 测试包并顺序运行，避免跨包同时清库。

本机已验证普通测试和 `go vet`。`go test -race` 还需要启用 CGO 并提供 C 编译器；当前环境缺少该条件，不能视作已通过 race 检查。

## M3 治疗与随访

迁移 4 增加治疗记录、任务、每任务照片和审计；迁移 5 增加预约 `completed` 状态；迁移 6 增加治疗版本与历史快照并回填原始记录。记录治疗、生成任务、完成预约和释放占号在同一事务提交，锁顺序保持患者 → 号源 → 预约。治疗记录绑定本预约的已发布方案，单预约唯一，相同请求内容可安全重试。

- 医护：`GET /admin/followups`，`GET /admin/appointments/:id/followups`，`POST /admin/appointments/:id/treatment`，`POST /admin/appointments/:id/treatment/corrections`，`POST /admin/followups/:taskId/review`。
- 患者：`GET /me/followups`，`GET /me/appointments/:id/followups`，`POST /me/followups/:taskId/submit`，`POST /me/followups/:taskId/photos/:angle`。
- 图片：`GET /me/followup-photos/:photoId/content` 或医护 `/admin/followup-photos/:photoId/content`，均需鉴权。医护接口每次核对最新授权，旧 v1 不覆盖 v2 的随访用途。

队列支持 `status`（含 `overdue`）、`page`、`pageSize`；分页上限 100。提交和回评带 `version`，相同已完成操作可恢复响应；不同内容或过期版本返回 409。异常只能通过 `handle` 且提供处理记录完成。规则及边界见根目录 `docs/M3随访规则与验收.md`。

治疗更正需当前 `version` 和 `correctionReason`；响应的 `treatmentRevisions` 包含原始及历次版本，患者只可读取本人历史。治疗时间变化仅会重排尚无反馈或照片的任务；已发生随访活动时返回 `TREATMENT_TIME_LOCKED`。

问诊、复诊日期范围查询、恢复日历范围查询及结构化治疗字段需执行数据库迁移21，详见 [问诊复诊与恢复日历接口](../../docs/问诊复诊与恢复日历接口.md)。
