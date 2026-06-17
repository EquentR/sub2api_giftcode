# 申请审批直充升级设计

## 目标

在现有“申请审批后发放兑换码”的流程上增加“直充到账”能力。

核心要求：

- 用户提交申请时可选择发放方式
- 发放方式有两个值：`direct_charge` 和 `redeem_code`
- 默认选择 `direct_charge`
- 管理员审批时必须遵循用户选择，不能临时修改
- 当用户选择 `direct_charge` 时，审批优先调用 Sub2API 的 `create-and-redeem` 接口
- 当 `direct_charge` 失败时，系统自动回退到现有“生成兑换码并发给用户”的流程
- 用户侧在直充成功后展示到账明细，不展示兑换码主体
- 用户侧仍保留兑换记录入口，便于查看回退发码或历史记录

## 非目标

- 不引入管理员直接修改用户余额的主流程
- 不引入管理员审批时手动覆盖用户发放方式的能力
- 不改变现有支付、邮件审批、档位管理和兑换码同步的大方向
- 不在本次设计里处理批量审批或规则自动审批

## 方案对比

### 方案一：用户提交时锁定发放方式，审批时严格执行

做法：

- 在 `redeem_access_requests` 上保存发放方式
- 用户提交时选择
- 管理员审批时只读展示
- 直充失败时自动回退发码

优点：

- 语义最清晰，申请单本身就是审批依据
- 审批链路可审计，后续容易追踪“用户要的是什么，系统最终怎么发的”
- 与当前 access request -> approve -> issue 的结构最贴合

缺点：

- 需要为申请单增加新的字段和结果展示

### 方案二：只记录最终处理结果，不记录用户原始选择

做法：

- 用户界面上可选直充或发码
- 后端不保存用户原始意图，只在审批时决定调用哪个流程

优点：

- 改动表面上更少

缺点：

- 审批单缺少关键业务语义
- 无法解释“为什么这单是直充/发码”
- 无法可靠阻止管理员在审批时临时改变方式

### 方案三：新增独立发放任务表承接直充和发码

做法：

- access request 审批后不直接发放
- 先写入独立 fulfillment job，再由 job 执行直充或发码

优点：

- 为后续重试、异步处理、多渠道发放留下更大空间

缺点：

- 对当前项目来说偏重
- 需要重构现有审批成功即发码的同步链路

## 推荐方案

采用方案一。

理由：

- 当前项目已经把“申请单”作为审批中心，发放方式天然属于申请单的一部分
- 方案一可以复用现有同步审批入口，只在审批内部增加“先直充，失败回退发码”的分支
- 改动范围可控，既能满足新需求，也不会把现有流程拆得过散

## 现状总结

当前流程是：

1. 用户在 `RechargeRequestView` 提交 access request
2. 后端 `CreateAccessRequest` 把档位快照写入 `redeem_access_requests`
3. 管理员在后台或邮件确认页点击审批
4. `ApproveAccessRequestByID` 调用 `issueRedeemRequest`
5. `issueRedeemRequest` 调用 Sub2API `POST /api/v1/admin/redeem-codes/generate`
6. 本地写入 `redeem_requests` 和 `redeem_codes`
7. access request 被标记为 `consumed`

也就是说，现在“审批成功”与“发码成功”是同一个动作，还没有“审批成功但选择直充”的分支。

## 上游接口能力

本次设计依赖以下 Sub2API 能力：

- 用户自己兑换：`POST /api/v1/redeem`
- 管理员生成兑换码：`POST /api/v1/admin/redeem-codes/generate`
- 管理员一步创建并为指定用户兑换：`POST /api/v1/admin/redeem-codes/create-and-redeem`

关键判断：

- `create-and-redeem` 不是直接改余额的旁路接口
- 它本质上是“创建兑换码 + 立即为指定用户核销”
- 因此它满足“优先尝试帮用户直接兑换兑换码”的要求

## 数据模型设计

### `redeem_access_requests`

新增字段：

- `fulfillment_mode TEXT NOT NULL DEFAULT 'direct_charge'`
- `fulfillment_result TEXT NOT NULL DEFAULT ''`
- `fulfilled_via TEXT NOT NULL DEFAULT ''`
- `fulfillment_error TEXT NOT NULL DEFAULT ''`

字段语义：

- `fulfillment_mode`
  - 用户申请时选择的目标发放方式
  - 允许值：`direct_charge`、`redeem_code`
  - 一旦创建不可修改
- `fulfillment_result`
  - 最终处理结果
  - 建议值：`direct_charge_succeeded`、`redeem_code_issued`
- `fulfilled_via`
  - 实际执行路径
  - 建议值：`direct_charge`、`redeem_code_fallback`、`redeem_code`
- `fulfillment_error`
  - 记录直充失败原因
  - 仅当 `direct_charge` 失败后回退发码时保存，便于审计和排障

设计原则：

- 把“用户想要的方式”和“系统最终怎么完成的”分开保存
- 这样才能区分“用户选发码”和“用户选直充但失败后改发码”

### `redeem_requests`

保持现有表为“发码动作账本”，但增加一层语义约束：

- 只有实际走了发码流程，才创建 `redeem_requests`
- 直充成功时不创建新的 `redeem_requests`
- 直充失败后回退发码时，照常创建 `redeem_requests`

这样能保证：

- 用户侧“兑换记录”仍可展示发码历史
- 直充成功不会伪造一条本地兑换码申请记录

### `redeem_codes`

继续只存真实下发给用户的兑换码：

- 发码模式成功时写入
- 直充失败回退发码时写入
- 直充成功时不写入

## 后端行为设计

### 创建申请

`CreateAccessRequest` 需要接受用户的发放方式，并把它与档位快照一起写入 `redeem_access_requests`。

要求：

- 请求体新增 `fulfillment_mode`
- 为空时后端按 `direct_charge` 处理
- 仅允许 `direct_charge` 或 `redeem_code`
- 保存后不允许更新

### 审批主流程

`ApproveAccessRequestByID` 改为按申请单的 `fulfillment_mode` 分流：

1. 读取 access request
2. 校验状态仍是 `pending` 或幂等可恢复状态
3. 读取对应档位快照
4. 如果 `fulfillment_mode == redeem_code`
   - 直接进入现有 `issueRedeemRequest`
5. 如果 `fulfillment_mode == direct_charge`
   - 先尝试 `issueDirectCharge`
   - 成功则结束
   - 失败则记录错误并自动进入 `issueRedeemRequest`

### 直充子流程

新增一个明确的服务层分支，例如：

- `issueDirectCharge`
- `approveWithDirectCharge`

职责：

- 组装 Sub2API `create-and-redeem` 入参
- 使用申请人 `upstream_user_id`
- 根据 `code_type` 决定传余额字段还是订阅字段
- 使用稳定幂等键
- 成功后更新 access request 的最终结果

请求映射：

- 余额档位
  - `type = balance`
  - `value = request.amount`
  - `user_id = request.requestor_upstream_user_id`
- 订阅档位
  - `type = subscription`
  - `user_id = request.requestor_upstream_user_id`
  - `group_id = request.sub2api_group_id`
  - `validity_days = request.validity_days`
  - `value` 沿用上游要求，按其接口约定填充

自定义 code：

- 为了满足上游幂等设计，直充调用应生成稳定、可重试的业务 code
- 建议使用基于本地申请单 ID 的 deterministic code，例如：
  - `giftcode-access-<access_request_id>`
- 这样同一申请重复审批时，上游能稳定返回同一结果而不是多次充值

### 直充失败后的回退

当 `create-and-redeem` 返回错误时：

1. 保存错误文本到 `fulfillment_error`
2. 不立即把申请标成失败
3. 继续执行现有 `issueRedeemRequest`
4. 发码成功后：
   - `fulfillment_result = redeem_code_issued`
   - `fulfilled_via = redeem_code_fallback`
   - `status = consumed`
5. 发码也失败后：
   - 保持 access request 不进入成功完成态
   - 将错误返回管理员
   - 允许后续幂等重试

这里最关键的业务原则是：

- “用户选直充”不是强制单点失败
- 只要最终能安全发码给用户，这单就算完成

### 幂等和重试

本次设计必须保留审批接口的重试安全。

直充路径：

- 为 `create-and-redeem` 使用稳定 `Idempotency-Key`
- 建议格式：
  - `giftcode-direct-charge-access-<id>-<token-hash>`

发码路径：

- 继续沿用现有 `redeemIssueIdempotencyKey`

重复审批时的目标行为：

- 若之前已直充成功，则再次审批返回已成功状态
- 若之前直充失败但发码成功，则再次审批返回同一兑换码
- 若之前直充失败且发码失败，则允许重试整条链路

## API 设计

### 用户创建申请

`POST /api/redeem-access-requests`

请求体新增：

- `fulfillment_mode?: "direct_charge" | "redeem_code"`

默认行为：

- 未传时按 `direct_charge`

响应体：

- 返回完整 access request
- 包含 `fulfillment_mode`、`fulfillment_result`、`fulfilled_via`、`fulfillment_error`

### 管理员审批

`POST /api/admin/redeem-access-requests/:id/approve`

响应结构需要从“只有 `request` + `code`”扩展成能表达两种结果：

- 若直充成功：
  - `request` 返回更新后的 access request
  - `code = null`
  - 增加 `fulfillment` 摘要，例如 `mode`, `result`, `fulfilled_via`
- 若发码成功：
  - 保持现有 `request` + `code`
  - 同时在 `request` 中体现是普通发码还是直充失败回退发码

推荐做法：

- 保持现有 `code?: RedeemCode | null`
- 把最终结果主要放到 `request` 字段上
- 这样前端改动更小

### 审批确认页

邮件确认页和前端确认页都应显示：

- 用户选择的发放方式
- 若为 `direct_charge`
  - 文案说明“优先直充，失败自动改为发码”
- 管理员界面不提供任何切换控件

## 前端设计

### 用户申请页

`RechargeRequestView.vue`

新增一个发放方式选择区：

- 单选项：
  - `直充到账（推荐）`
  - `下发兑换码`
- 默认值为 `直充到账`

文案建议：

- `直充到账（推荐）`
  - 审批通过后优先直接充入你的 Sub2API 账户
  - 如直充失败，系统会自动改为下发兑换码
- `下发兑换码`
  - 审批通过后下发兑换码，你手动兑换

### 用户申请状态区

列表中除了状态，还要展示最终发放结果：

- 直充成功：
  - `已直充到账`
  - 明细显示金额或订阅、生效时间、处理时间
- 普通发码成功：
  - `已发放兑换码`
- 直充失败后回退发码：
  - `直充失败，已改为发码`

当 `code` 不存在时：

- 不再统一显示“暂无关联兑换码”
- 对直充成功的申请改为显示到账结果说明

### 用户总览页

`UserDashboardView.vue`

现有“已发放兑换码”指标不再能完全代表所有成功申请。

建议调整为：

- 保留兑换码统计
- 新增成功直充统计
- 文案从“每个已审批申请都会对应一个兑换码”改为“每个已完成申请会以直充或兑换码的形式交付”

### 管理员审批队列

`AdminAccessQueueView.vue`

需要新增展示字段：

- 用户选择的发放方式
- 最终处理结果

审批弹窗行为：

- 显示发放方式，但不可编辑
- 若审批成功且结果是直充：
  - 弹窗展示“已直充到账”
  - 展示到账明细
  - 不展示兑换码输入框
- 若审批成功且结果是发码：
  - 维持现有兑换码展示

### 邮件确认页

`ApprovalConfirmView.vue`

当前文案默认是“确认审批并发码”，需要改成更中性的审批文案：

- 标题从“确认审批并发码”改为“确认审批并处理申请”
- 当方式是 `direct_charge` 时，说明“系统将优先直充，失败自动发码”
- 当方式是 `redeem_code` 时，说明“系统将直接发码”

## 文案方向

为了避免让用户以为直充一定不会产生兑换码，需要把“失败自动回退发码”写清楚，但不要过度强调失败。

推荐用语：

- 用户申请页：`直充到账（推荐）`
- 管理员审批结果：
  - `已直充到账`
  - `已发放兑换码`
  - `直充失败，已改为发码`

避免继续使用：

- `审批并发码`
- `每个已审批申请都会对应一个兑换码`

## 状态与结果矩阵

### 申请单状态

继续保留现有：

- `pending`
- `approved`
- `rejected`
- `expired`
- `consumed`

说明：

- `status` 表示审批生命周期
- `fulfillment_result` 和 `fulfilled_via` 表示交付结果

不要把“直充成功”“发码成功”塞进 `status`，避免与现有审批语义混淆。

### 典型结果

1. 用户选 `redeem_code`
   - `status = consumed`
   - `fulfillment_result = redeem_code_issued`
   - `fulfilled_via = redeem_code`
   - 存在 `redeem_request` 和 `redeem_code`

2. 用户选 `direct_charge` 且成功
   - `status = consumed`
   - `fulfillment_result = direct_charge_succeeded`
   - `fulfilled_via = direct_charge`
   - 不存在新的 `redeem_request` 和 `redeem_code`

3. 用户选 `direct_charge` 但失败后回退发码成功
   - `status = consumed`
   - `fulfillment_result = redeem_code_issued`
   - `fulfilled_via = redeem_code_fallback`
   - 存在 `redeem_request` 和 `redeem_code`
   - `fulfillment_error` 保存直充失败原因

## 错误处理

### 直充失败

- 记录上游错误
- 自动尝试发码
- 若发码成功，对管理员返回成功结果而不是失败

### 直充和发码都失败

- 返回审批失败
- access request 保持可重试状态
- 本地错误信息要能区分“直充失败”和“发码失败”

### 非法订阅参数

当订阅申请缺失：

- `group_id`
- `validity_days`

应直接中止，不进入直充或发码。

### 幂等冲突

如果上游返回同一 code 已用于其他用户：

- 视为严重冲突
- 不允许静默继续
- 返回管理员处理

## 测试设计

### 后端

新增或调整测试覆盖：

- 创建 access request 时默认 `fulfillment_mode = direct_charge`
- 创建 access request 时可保存 `redeem_code`
- 审批 `redeem_code` 模式继续走原有发码逻辑
- 审批 `direct_charge` 模式成功时不创建本地兑换码记录
- 审批 `direct_charge` 模式失败后自动回退发码
- 回退发码成功时保存 `fulfillment_error`
- 直充和发码都失败时返回失败且申请可重试
- 审批重复调用时：
  - 直充成功可幂等返回
  - 回退发码成功可返回同一兑换码

### 前端

新增或调整测试覆盖：

- 用户申请页默认选中 `direct_charge`
- 用户可切换到 `redeem_code`
- 管理员审批弹窗只显示发放方式，不允许编辑
- 直充成功时显示到账明细，不显示兑换码输入框
- 回退发码成功时显示兑换码和正确提示文案
- 用户状态区根据 `fulfilled_via` 渲染不同结果

## 受影响文件范围

后端重点：

- `backend/internal/db/migrate.go`
- `backend/internal/models/models.go`
- `backend/internal/app/access.go`
- `backend/internal/app/redeem.go`
- `backend/internal/sub2api/client.go`
- `backend/internal/httpapi/access_handlers.go`
- `backend/internal/httpapi/types.go` 或对应请求响应结构文件
- `backend/internal/app/*_test.go`
- `backend/internal/httpapi/*_test.go`

前端重点：

- `frontend/src/api/types.ts`
- `frontend/src/api/access.ts`
- `frontend/src/views/RechargeRequestView.vue`
- `frontend/src/views/AdminAccessQueueView.vue`
- `frontend/src/views/ApprovalConfirmView.vue`
- `frontend/src/views/UserDashboardView.vue`
- `frontend/src/utils/approval-details.ts`
- 相关前端测试文件

## 假设

- 当前接入的 Sub2API 版本已包含 `POST /api/v1/admin/redeem-codes/create-and-redeem`
- 该接口在本项目部署目标环境中可由 admin api key 访问
- 当前本地数据库允许继续通过 migration 增量加列
- 直充成功后不需要额外同步一条“虚拟兑换码记录”到本地

## 风险

- 如果部署中的 Sub2API 版本低于已调研版本，`create-and-redeem` 可能不存在
- 直充成功但本地记录更新失败时，需要小心幂等恢复，避免重复充值
- 用户界面若只按“是否存在兑换码”判断状态，会误把直充成功显示成“暂无兑换码”

## 结论

这次升级应该把“发放方式”提升为申请单的一等属性。

最小可行改造是：

- 在 access request 上保存用户选择
- 审批时优先尝试 `create-and-redeem`
- 失败后无缝回退到现有发码链路
- 前端把“完成申请”从“拿到兑换码”扩展为“直充到账或拿到兑换码”

这样既能满足“默认直充、用户可改发码”的产品诉求，也能最大化复用现有审批发码系统。
