# 订阅重置次数赠送与补偿延期设计

## 背景

订阅额度重置功能上线后，旧数据自动补发需要根据本地购买记录、上游订阅历史、撤销状态、外部延期和批量补偿反推周期边界。生产环境已经出现无法唯一对齐的记录：Std 补发任务共有 4 条周期，只处理并授予 2 条，另外 2 条持续重试；其中当前有效订阅也被旧的已撤销订阅连带阻塞。

继续扩展历史自动推断会引入订阅代际匹配、歧义决议和复杂的数据修复逻辑。该复杂度与实际目标不成比例。历史缺失权益可以通过管理员明确赠送解决，营销活动也需要同样的重置次数发放能力。

因此本设计停用旧数据自动补发，新增独立的“重置次数赠送”功能。同时保留批量补偿对权益有效期的影响：补偿天数延长已有重置次数的有效期，但不增加次数。

## 已确认规则

- 现有批量补偿功能保持独立，`subscription_days` 和 `balance_amount` 仍然都必须大于 0。
- 重置次数赠送使用独立接口、页面、批次和权益账本。
- 赠送支持全部符合条件用户或指定用户。
- 每次活动必须选择至少一个订阅分组，并允许多选。
- 无限额分组不能参加重置次数赠送。
- 指定分组中只要存在有效上游订阅即可获赠，不要求存在本地购买周期。
- 赠送权益绑定具体 `subscription_id`，不跨订阅转移。
- 赠送时的有效期等于该订阅当前到期时间。
- 后续批量补偿延长该订阅时，购买次数和赠送次数的有效期同步顺延，次数不增加。
- 订阅撤销或失效后，未使用赠送次数失效。
- 多种权益同时可用时，按最早到期优先扣减；到期时间相同时先使用赠送次数。
- 旧自动补发停止运行；已成功授予的次数保留，尚未授予的部分通过赠送功能处理。

## 目标

- 提供可审计、可重试、幂等的管理员重置次数赠送能力。
- 支持全部用户、指定用户和一个或多个有限额订阅分组。
- 允许没有本地购买周期的有效外部订阅获得营销赠送。
- 在用户订阅卡片中聚合基础次数和赠送次数。
- 在重置事务中精确记录并扣减实际权益来源。
- 将批量补偿的上游延期同步到本地购买周期和赠送权益。
- 停止复杂且不可靠的旧数据自动补发和历史周期反推。

## 非目标

- 不把重置次数输入加入现有批量补偿接口或表单。
- 不改变现有批量补偿的“订阅天数和余额都必须大于 0”校验。
- 不为无限额订阅创建重置权益。
- 不把赠送次数转移到用户后来创建的其他订阅。
- 不自动恢复已撤销或已过期订阅的权益。
- 不继续实现历史订阅代际自动匹配、歧义绑定或旧周期 dry-run 修复。

## 总体架构

系统保留现有 `subscription_reset_periods`，用于新兑换产生的基础重置次数。新增营销赠送批次和 bonus grant 账本。

查询订阅和执行重置时，服务聚合：

1. 当前购买周期剩余次数。
2. 当前 `subscription_id` 下所有有效 bonus grant 的剩余次数。

外部订阅没有本地购买周期时，只要存在有效 bonus grant，也可以执行额度重置。`external_period` 只能表示没有基础购买周期，不能覆盖有效赠送权益的可用状态。

## 数据模型

### `subscription_reset_bonus_batches`

记录一次赠送活动：

- `id`
- `batch_key`：全局唯一幂等键
- `target_scope`：`all` 或 `selected`
- `selected_user_ids_json`：指定用户模式的不可变快照
- `group_ids_json`：一个或多个目标分组的不可变快照
- `reset_count`：每个目标订阅获赠次数
- `note`
- `status`：`pending`、`running`、`completed`、`completed_with_failures`、`failed`
- `total_candidates`
- `processed_candidates`
- `granted_subscriptions`
- `skipped_subscriptions`
- `failed_subscriptions`
- `operator_upstream_user_id`、`operator_email`、`operator_username`
- `error_message`
- `created_at`、`started_at`、`completed_at`、`updated_at`

约束：

- `batch_key` 唯一。
- `target_scope = selected` 时用户列表不能为空。
- 分组列表不能为空且去重。
- `reset_count > 0`。
- 批次输入在创建后不可修改。

### `subscription_reset_bonus_batch_details`

记录每个候选订阅的处理结果：

- 批次 ID
- 用户、分组和上游订阅 ID
- 上游订阅开始、到期和状态快照
- 处理状态：`pending`、`granted`、`skipped`、`failed`
- 稳定原因和错误详情
- 对应 bonus grant ID
- 创建和更新时间

同一批次、同一上游订阅最多一条明细。后台任务根据明细状态中断恢复，不重新处理已经完成的目标。

### `subscription_reset_bonus_grants`

保存实际赠送权益：

- `id`
- `batch_id`、`batch_detail_id`
- `upstream_user_id`
- `sub2api_group_id`
- `upstream_subscription_id`
- `reset_limit`
- `reset_used`
- `starts_at`
- `expires_at`
- `status`：`active`、`exhausted`、`expired`、`revoked`
- `subscription_snapshot_json`
- `last_synced_at`、`last_error`
- `created_at`、`updated_at`

约束：

- 同一批次不能对同一订阅重复赠送。
- `reset_limit > 0`。
- `0 <= reset_used <= reset_limit`。
- `starts_at < expires_at`。

同一订阅可以从不同营销批次获得多条 grant，每条独立审计和过期。

### `subscription_extension_events`

记录批量补偿产生的上游订阅延期：

- `id`
- `event_key`：使用补偿批次、用户和订阅生成的唯一幂等键
- `source_type`：`compensation` 或 `legacy_compensation`
- `compensation_batch_id`、`compensation_detail_id`
- 用户、分组和上游订阅 ID
- 延期天数
- 延期前和延期后到期时间
- `status`：`reserved`、`succeeded`、`failed`、`uncertain`
- `resolution`：空、`applied` 或 `released`
- 已应用的基础周期和 bonus grant 数量
- `inferred_from_legacy`、`migration_version`
- 错误、预留、完成、确认和操作人字段
- `created_at`、`updated_at`

同一补偿明细和订阅最多一条延期事件。新产生的 `compensation` 事件成功时必须具有延期前后快照；`legacy_compensation` 允许快照为空，但必须保留来源明细、延期天数和迁移版本。成功事件的权益修改与 `resolution = applied` 在同一 SQLite 事务中完成，事务失败时全部回滚，因此不能只依据应用数量判断是否已经应用。

### 重置操作来源

调整 `subscription_reset_attempts`：

- `period_id` 改为可空。
- 增加 `entitlement_type`：`base_period` 或 `bonus_grant`。
- 增加 `entitlement_id`。
- `entitlement_type + entitlement_id` 必须指向真实权益。
- 阻塞中的部分唯一约束改为 `upstream_subscription_id`，保证同一订阅最多一条 `reserved/uncertain` 操作。

已有操作迁移为 `entitlement_type = base_period`，`entitlement_id = period_id`。

### 历史周期忽略字段

为 `subscription_reset_periods` 增加：

- `legacy_ignored`
- `legacy_ignored_at`
- `legacy_ignore_reason`

停用旧自动补发时，只把尚未授予、`reset_limit = 0` 且属于上线前历史补发范围的周期标记为忽略。周期创建、排期和边界推断查询必须排除 `legacy_ignored = 1`；已经拥有正数权益的周期不得被忽略。

## 赠送预览

新增只读预览接口。输入包括：

- `target_scope`
- 指定用户 ID 列表
- 分组 ID 列表
- `reset_count`
- 备注

预览执行以下检查：

- 管理员权限。
- 指定用户存在且去重。
- 分组存在、有效、至少配置一个额度窗口，并去重。
- 赠送次数为正整数。
- 从 Sub2API 分页读取目标用户的有效订阅。

返回：

- 预计用户数和订阅数。
- 按分组统计。
- 无有效目标订阅的指定用户。
- 被跳过的无限额、失效或分组不匹配订阅。
- 预览摘要哈希。
- 10 分钟有效的预览令牌和过期时间。

预览不写入批次、grant 或操作账本。预览令牌由后端签名，绑定管理员、输入参数、候选订阅摘要和过期时间。

## 赠送批次执行

管理员确认预览后创建持久化批次。创建接口返回 `202`，后台 worker 执行：

1. 校验预览令牌未过期、操作人一致，并重新读取目标范围；候选摘要变化时返回 `preview_stale`，要求重新预览。
2. 固化目标用户、分组、次数、备注和预览摘要。
3. 创建候选明细。
4. 对每个候选重新读取或校验上游订阅归属、分组和有效状态。
5. 在本地事务中创建 bonus grant 并把明细标记为 `granted`。
6. 单个目标失败只更新该明细，不回滚其他目标。
7. 汇总完成后标记批次状态。

服务启动时恢复 `pending/running` 批次，只处理仍为 `pending` 的明细。唯一约束保证崩溃重试不重复赠送。

稳定跳过原因包括：

- `user_not_found`
- `no_active_subscription`
- `group_not_selected`
- `unlimited_group`
- `subscription_inactive`
- `preview_stale`

上游订阅列表整体读取失败时批次保持可重试；单个订阅读取失败记录为目标失败。

## 权益聚合与展示

`GET /api/subscriptions` 为每张订阅卡片返回：

- 基础周期总次数、已用次数和剩余次数。
- bonus grant 总剩余次数。
- 合计可用次数。
- bonus 明细：批次 ID、备注、剩余次数和到期时间。
- 下一条将被扣减的权益来源和到期时间。

按钮资格规则调整为：

- 有基础次数或 bonus 次数即可进入次数校验。
- 没有基础周期但存在有效 bonus 时，`external_period = true` 只作为信息展示，不能禁用重置。
- 无限额、订阅失效、无用量、上游不可用和操作确认中仍按现有稳定原因禁用。
- 基础与 bonus 合计为零时返回 `reset_exhausted` 或 `zero_reset_limit`。

用户界面显示基础次数、活动赠送次数和合计，不向用户暴露管理员内部错误或用户列表。

## 重置扣减事务

执行重置前，后端重新读取权威订阅和额度窗口，并加载全部可用权益 bucket：

- 当前有效基础周期。
- 当前订阅下 `active` 且未耗尽的 bonus grant。

排序规则：

1. `expires_at` 最早优先。
2. 到期时间相同时 bonus grant 优先。
3. 同类型、同到期时间按权益 ID 升序。

在 SQLite 事务中：

1. 检查同一上游订阅没有阻塞操作。
2. 对选中的权益 bucket 原子增加一次 `reset_used`。
3. 插入 `reserved` 操作并记录权益类型和 ID。
4. 提交后调用上游重置。

上游成功保留扣减；明确失败在一个事务中归还到原权益；未知结果继续占用。幂等重放返回原操作和同一权益来源，不重新选择 bucket。

人工 `released` 决议同样归还到原权益。bonus 用尽后状态更新为 `exhausted`。

## 批量补偿延期

现有批量补偿接口和表单保持原样：

- `subscription_days > 0`
- `balance_amount > 0`
- 有有效订阅的用户延长订阅；否则按现有规则补余额。

对每个订阅延期时增加本地事件事务：

1. 权威读取延期前订阅状态。
2. 写入 `reserved` 延期事件并提交。
3. 使用相同幂等键调用上游延期接口。
4. 成功时保存新到期时间并标记 `succeeded`。
5. 明确失败标记 `failed`，不改变权益。
6. 结果未知标记 `uncertain`，不自动重复调用上游。

成功延期事件按 `subscription_id` 原子应用：

- 当前基础周期 `period_end` 延长相同天数。
- 当前周期之后的已排期周期整体向后平移相同天数。
- 所有尚未过期的 bonus grant `expires_at` 延长相同天数。
- `reset_limit` 和 `reset_used` 均保持不变。

如果延期时尚无基础周期或 bonus grant，事件仍标记成功；后续创建或绑定权益时不得把早于权益创建的延期重复应用。

`uncertain` 事件不得提前改变本地有效期。只有能够排除其他延期来源并证明本次上游操作成功时才自动确认，否则由管理员选择 `applied` 或 `released`，且不再次调用上游。

补偿结束后非阻塞唤醒重置权益同步 worker。

## 停用旧自动补发

旧数据自动补发机制停止：

- 档位从 `reset_count = 0` 改为正数时不再创建 `subscription_reset_backfill_runs`。
- 后台对账不再处理旧补发任务。
- 现有 `pending/running/failed` 任务迁移为 `superseded`，保存停用时间和原因。
- 已成功授予到 `subscription_reset_periods` 的次数保留，不回收、不重复赠送。
- 未授予的历史零次数周期标记为 `legacy_ignored`，不再参与周期边界推断。
- 新功能上线后的购买周期继续按档位快照创建和排期。

生产环境处理：

- 用户 5、6 已成功获得的 Std 3 次保留。
- 用户 1 当前 Std 订阅通过一次“指定用户 + Std 分组 + 3 次”的 bonus 批次补齐。
- Pro 已撤销订阅不赠送。
- Plus 没有缺失记录，不赠送。
- 原 Std 失败任务标记为 `superseded`，停止继续重试。

## 历史补偿迁移

已有补偿批次明细记录了成功延期的用户、订阅 ID 和天数。迁移按来源明细幂等生成历史 `succeeded` 延期事件：

- 已知 `+3 天` 和 `+1 天`分别生成事件。
- 不虚构缺失的延期前后快照。
- 对当前仍存在且已绑定的基础周期累计延长 4 天。
- 当时尚不存在 bonus grant，因此历史事件不能延长后来创建的营销赠送。
- 上游跨度中其他无法解释的天数保持为外部时间，不创建次数。

历史事件必须标记迁移版本；重复迁移不能再次顺延周期。

## 管理员接口

新增：

- `POST /api/admin/subscription-reset-bonus-batches/preview`
- `POST /api/admin/subscription-reset-bonus-batches`
- `GET /api/admin/subscription-reset-bonus-batches`
- `GET /api/admin/subscription-reset-bonus-batches/:id/details`
- `GET /api/admin/subscription-extension-events`
- `POST /api/admin/subscription-extension-events/:id/resolve`

创建批次只接受服务端生成或验证的预览摘要。客户端不能传入 `subscription_id` 列表扩大预览范围。

所有接口继续使用现有管理员权限中间件。批次和人工决议保存操作人，冲突决议返回 `409`。

## 管理员界面

新增独立导航页“重置次数赠送”，包含：

- 全部用户和指定用户模式切换。
- 指定用户搜索、多选和已选数量。
- 有限额订阅分组多选。
- 赠送次数和活动备注。
- 预览统计、跳过原因和二次确认。
- 批次进度、历史和目标明细。

现有批量补偿页面不增加重置次数输入，只增加延期事件结果和异常提示入口。

管理员页面不得展示或发送 Sub2API 管理 API Key。

## 启动、同步与恢复

新增 bonus batch worker 和 extension event worker：

- 服务启动时立即恢复未完成任务。
- 使用现有同步间隔轮询。
- 创建赠送批次、完成补偿和人工决议后非阻塞唤醒。
- 同一 worker 使用串行锁，数据库唯一约束作为最终并发保护。

订阅同步时更新 grant 状态：

- 当前时间达到 `expires_at` 后标记 `expired`。
- 上游订阅撤销后标记 `revoked`。
- 次数用尽标记 `exhausted`。
- 上游读取失败保留最后确认状态，但用户端禁用重置并显示 `upstream_unavailable`。

## 数据迁移

迁移必须支持旧库重复执行：

1. 创建 bonus batch、detail、grant 和 extension event 表及索引。
2. 扩展重置操作的权益来源字段和阻塞索引。
3. 重建或扩展旧补发任务状态约束，使其支持 `superseded`。
4. 已有操作回填为基础周期来源。
5. 停用未完成旧补发任务。
6. 标记未授予的历史零次数周期为 `legacy_ignored`。
7. 从历史补偿明细生成延期事件并幂等应用。

迁移不得自动创建营销赠送批次。生产用户 1 的 3 次补齐必须由管理员预览并确认执行。

## 测试

数据库测试覆盖：

- 旧库和重复迁移。
- bonus 批次、明细和 grant 唯一约束。
- 操作权益来源迁移。
- 旧补发任务停用且不再重试。
- 历史延期事件只应用一次。

服务测试覆盖：

- 全部用户、指定用户、单分组和多分组。
- 用户多订阅时每个选中分组分别赠送。
- 没有本地周期的外部订阅获得赠送。
- 无限额、失效订阅和无目标用户跳过。
- 批次崩溃恢复和重复创建幂等。
- 基础与 bonus 最早到期扣减顺序。
- 上游成功、明确失败、未知结果和人工释放归还原权益。
- 补偿延长当前周期、顺延未来周期并延长现有 bonus。
- 补偿不增加次数，历史补偿不影响后来创建的 grant。
- 上游撤销和过期同步。

HTTP 和前端测试覆盖：

- 管理员权限和输入校验。
- 预览与执行范围一致，过期预览拒绝执行。
- 全部/指定用户及分组多选交互。
- 二次确认、批次轮询和失败明细。
- 用户卡片基础、赠送和合计次数展示。
- 移动端表单、确认框和明细不裁切。

最终运行后端全量测试、前端测试与构建，并在生产数据库备份副本上验证迁移、旧任务停用和历史延期事件。

## 上线步骤

1. 备份生产 SQLite 主文件、WAL 和 SHM。
2. 在备份副本上执行迁移并核对旧任务、已有权益和历史补偿事件。
3. 部署新程序，确认旧补发任务不再重试。
4. 检查用户 5、6 已有次数保留且有效期叠加历史补偿。
5. 在“重置次数赠送”页面预览指定用户 1、Std 分组、3 次。
6. 核对仅命中当前有效 Std 订阅后执行。
7. 验证用户 1 卡片显示赠送 3 次并能够重置。
8. 执行一笔测试补偿，确认基础周期、未来周期和已有 bonus 的有效期同步顺延，次数不变。
9. 观察至少两个同步周期，确认批次、grant 和延期事件状态稳定。

回滚时先停止新版本服务，再恢复数据库备份和旧程序。不得只回滚程序而保留已迁移的数据结构和权益修改。

## 验收标准

- 旧自动补发任务永久停止，不再产生重试噪音。
- 已成功补发次数不丢失，未补发权益可通过管理员赠送补齐。
- 管理员能向全部或指定用户、一个或多个有限额分组赠送次数。
- 外部有效订阅可以只依赖 bonus grant 执行重置。
- 同一批次、同一订阅不会重复获赠。
- 重置严格按最早到期顺序扣减并能向原权益归还。
- 批量补偿延长基础和已有 bonus 的有效期，但不增加次数。
- 用户 1 当前 Std 订阅通过独立赠送获得 3 次，用户 5、6 不重复获赠。
