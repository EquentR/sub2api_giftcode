# 订阅重置权益总览与赠送明细实施计划

依据 `docs/superpowers/specs/2026-07-18-subscription-reset-entitlement-overview-design.md` 实施。每个阶段遵循测试先行：先加入能复现缺口的失败测试，再完成实现。主代理自测后停止修改，由常驻只读 reviewer 独立检查 diff 和验证命令；只有 reviewer 返回 `PASS` 才提交阶段检查点并进入下一阶段。

## 1. Sub2API 全量有效订阅读取

- 扩展 Sub2API 客户端，分页读取全部 `active` 订阅，保留用户和分组显示信息，并拒绝无法权威验证的分页结果。
- 复用管理员 API Key、既有响应解析和脱敏错误分类，不读取逐订阅额度进度。
- 验证：覆盖多页、权威空页、异常分页、缺失显示字段、请求路径/查询参数/鉴权和网络失败；运行 `cd backend; go test ./internal/sub2api -run 'Test.*(AllActiveSubscriptions|SubscriptionsPagination)'`。
- Reviewer 门禁：部分结果可能被当作完整列表、非管理员鉴权、分页终止条件不可靠或敏感信息泄露均为 `BLOCKED`。

## 2. 管理员权益聚合 API

- 在应用层新增订阅权益汇总 DTO 和批量聚合服务：按有效上游订阅关联当前基础周期，以及满足 `starts_at <= now < expires_at` 的 `active/exhausted` 赠送记录。
- 计算基础总数/已用/剩余、赠送总数/已用/剩余及合计可用；保留外部订阅和合计为零的订阅，排除未来、过期与撤销赠送，并按用户、分组、订阅稳定排序。
- DTO 完整返回用户 ID/用户名/邮箱、订阅 ID、分组 ID/名称、订阅起止时间、剩余天数及基础/赠送/合计字段；显示名称缺失时仍保留对应 ID。
- 新增 `GET /api/admin/subscription-reset-entitlements`，上游读取失败映射为 `502 upstream_unavailable`，本地查询失败返回 `500`，继续使用管理员路由鉴权。
- 验证：覆盖基础周期边界、多笔赠送、未来赠送、未到期已用完记录、过期/撤销记录、外部订阅、零次订阅、稳定排序、完整响应契约、显示字段缺失、502/500 和普通用户越权；运行 `cd backend; go test ./internal/app ./internal/httpapi -run 'Test.*SubscriptionResetEntitlements'`。
- Reviewer 门禁：N+1 SQLite 查询、未来赠送被提前计入、响应字段不完整、错误被伪装成零、口径或排序与 spec 不符、普通用户可访问均为 `BLOCKED`。

## 3. 管理员“当前订阅权益”界面

- 扩展前端类型和管理员 API 模块，在“赠送重置”页面加入独立的权益表格、加载状态、错误状态和刷新按钮。
- 展示用户、订阅、到期、基础剩余、赠送剩余和合计可用；增加用户关键词搜索与分组筛选，零次订阅弱化但不隐藏。
- 将权益刷新接入现有 15 秒轮询；静默失败保留旧数据并提示数据可能过期，不影响赠送表单、批次历史或延期事件。
- 验证：覆盖请求路径、字段渲染、组合筛选、零次展示、显示名称回退、手动刷新与静默失败保留旧数据；运行 `cd frontend; pnpm test` 和 `cd frontend; pnpm build`。
- Reviewer 门禁：任一加载失败拖垮页面其他区域、轮询清空旧数据、筛选漏项或零次订阅被隐藏均为 `BLOCKED`。

## 4. 用户赠送汇总与明细弹框

- 调整订阅卡片：保留基础、活动赠送和合计三个汇总，只显示一个赠送剩余数量，并在存在未到期赠送记录时提供“查看详情”文字按钮。
- 新增赠送明细弹框，展示说明回退、获赠总数、已用、剩余、到期时间及“可用/已用完”；包含未到期的 `active/exhausted` 记录，不展示内部管理信息。
- 轮询后按订阅 ID 更新已打开弹框；订阅消失时关闭并提示。卡片设置 `width: 100%`、`max-width: 630px`，桌面保持等高多列，移动端保持单列满宽，无限额卡片高度不变。
- 验证：覆盖详情按钮显隐、已用完记录、空说明、弹框轮询同步、订阅消失、原重置按钮/禁用原因不回归；运行 `cd frontend; pnpm test` 和 `cd frontend; pnpm build`。
- Reviewer 门禁：卡片仍铺开逐笔记录、弹框遗漏已用完记录、轮询显示陈旧订阅、卡片超过 630px 或移动端溢出均为 `BLOCKED`。

## 5. 集成、视觉与最终回归

- 使用固定数据场景覆盖：基础加赠送、外部订阅、合计零次、多笔赠送、已用完未到期、过期/撤销、缺少显示名称和上游不可用。
- 使用 Playwright 在 `1440x900` 和 `390x844` 检查管理员表格、搜索/筛选、用户卡片和赠送明细弹框，确认无重叠、裁切、异常拉伸或状态残留。
- 完整运行 `cd backend; go test -race ./...`、`cd frontend; pnpm test`、`cd frontend; pnpm build`，并确认没有数据库迁移、重置资格或扣减逻辑改动。
- Reviewer 独立复跑全量测试、检查最终 diff 和视觉证据；仅在所有阶段均 `PASS` 且工作树没有未解释修改后声明完成。不自动合并、推送、部署或修改线上数据。

## 常驻 Reviewer 协议

- 固定 reviewer 为 `/root/entitlement_reviewer`，全程只读，不修改共享工作树、不提交、不切换分支。
- 每阶段主代理记录当前 HEAD、`git status --short`、阶段 diff 和自测结果，再通过 `followup_task` 唤醒 reviewer。
- Reviewer 只返回 `PASS` 或 `BLOCKED`；`BLOCKED` 必须给出 `文件:行`、复现方式、预期行为和最小验证命令。
- 主代理修复后必须对同一阶段重新送审；未获 `PASS` 不提交检查点、不进入下一阶段。
- 当前协作工具不提供模型选择参数，无法验证 reviewer 实际运行模型；reviewer 按用户要求采用 medium 深度审核协议。
