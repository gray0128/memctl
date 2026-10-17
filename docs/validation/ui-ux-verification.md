# ego-browser 端到端 UI/UX 验证报告

本报告记录一次基于 ego-browser 的 `memctl ui` 端到端 UI/UX 验证，以及由此登记的收口任务 `T039` 的修复范围与复验结果。验证以真实二进制、真实 Git 仓库和浏览器会话执行，不使用桩服务。

## 1. 验证环境与步骤

- 二进制：`go build -o bin/memctl ./cmd/memctl`，前端为当轮源码构建的 `web/dist`。
- 仓库：临时 HOME 与临时记忆仓库；读写服务 `memctl ui --no-open`（`127.0.0.1:18420`），只读服务加 `--readonly`（`127.0.0.1:18421`）。
- 冲突场景：本地裸远端加 peer 克隆，两端修改同一行后 `memctl sync --wait` 捕获冲突，再由 UI 的 `POST /api/v1/conflict:resolve` 解决。
- 覆盖：概览、知识图谱、条目、冲突解决、审计、诊断、整理接入七个视图；条目过滤、条目原文编辑、条目移动与删除、文件原文编辑与 412 草稿保留、审计 Diff 与回滚、adapter 计划预览、Dream 查询错误、只读模式、深色模式、快照错误态与恢复、390 px 窄屏。
- 通过项：上述交互均按 `docs/04-ui-and-cli-specification.md` 第 4 节工作，七视图遍历期间无 JS 异常、无失败 API 请求、无横向溢出。
- 说明：本轮为一次性视觉交互验证，截图保存在会话临时目录，未提交；长期可复核证据是第 3 节的自动化回归。

## 2. 发现的问题

| ID | 级别 | 现象 | 根因位置（修复前） |
| --- | --- | --- | --- |
| `P2-1` | P2 | 外部写入后，UI 有时停在旧计数并显示「快照已过期」，直到刷新或手动同步才恢复 | watcher 在写入方仍持锁时发布 `vault.updated`，客户端立即重读 `GET /api/v1/snapshot`，命中保护态返回 `stale: true` 缓存；客户端不再重试 |
| `P2-2` | P2 | 冲突解决成功后，顶栏仍显示「远端状态 conflict」「领先 1 · 落后 1」，与实际已同步不符 | `finishResolve`/`ResolveAbort`/`staleConflict` 只清除冲突状态，未更新 `$STATE` 的同步摘要缓存 |
| `P3-1` | P3 | 无上游仓库手动同步提示「领先 0 · 落后 0」，而顶栏同一信息显示「未知」 | `POST /api/v1/sync` 的 `aheadCount`/`behindCount` 用非指针 `int`，无法表达未知 |
| `P3-2` | P3 | 知识图谱节点标签互相压盖，长路径不可读 | 节点标签使用完整路径，cose 默认斥力与容器高度不足 |
| `P3-3` | P3 | 390 px 窄屏顶栏字段竖排换行、值截断、同步按钮插在字段网格中间 | 顶栏在 `sm` 断点以下缺少单列布局 |

## 3. 修复与复验

`T039` 收口上述五项，契约与实现对应关系如下。

- `P2-1`：新增 `web/src/refresh.js` 的 `loadFreshSnapshot`，对 `stale: true` 的响应做有界指数退避重试；`web/src/app.jsx` 的 `refresh` 与 `refreshSnapshot` 改用该函数。契约见 [Web UI](../04-ui-and-cli-specification.md#4-web-ui)。复验：连续 6 次外部 `memctl write`，UI 文件计数每次都收敛到服务端值，无残留过期横幅。
- `P2-2`：`internal/app/resolve.go` 新增 `persistResolveSyncState`，在 `finishResolve`、`ResolveAbort`、`staleConflict` 更新同步摘要缓存；无上游时记为 `local_only`，有上游时按 `git.AheadBehind` 写入计数。复验：UI 解决冲突后顶栏显示「远端状态 synced」「领先 0 · 落后 0」，快照 `syncState` 同步变化。
- `P3-1`：`internal/server/writes.go` 的 `syncSummaryDTO` 改用 `*int`，`hasUpstream` 为假时输出 `null`。契约见 [同步与恢复](../02-domain-models-and-contracts.md#22-同步与恢复)。复验：无上游仓库提示「领先 未知 · 落后 未知」，与顶栏一致。
- `P3-2`：`web/src/graph.js` 新增 `graphLabel`，节点标签使用文件名、`fullPath` 保留完整路径；`KnowledgeGraph.jsx` 增加标签省略上限、节点斥力与容器高度，选中面板显示完整路径。契约见 [Web UI](../04-ui-and-cli-specification.md#4-web-ui)。复验：连通子图标签不再重叠。
- `P3-3`：`web/src/components/TopBar.jsx` 在 `sm` 以下改为纵向布局与整行状态网格，同步按钮独占一行。复验：390 px 下字段单行显示、无横向溢出。

自动化回归：

- `tests/integration/t039_ui_state_test.go`：`TestT039SyncSummaryHasUnknownCountsWithoutUpstream` 覆盖 `P3-1`，`TestT039ConflictResolveRefreshesSyncState` 覆盖 `P2-2`。
- `web/src/refresh.test.js`：覆盖 `P2-1` 的重试、退避、耗尽与错误传播。
- `web/src/graph.test.js`：覆盖 `P3-2` 的文件名标签与完整路径保留。

## 4. 未覆盖与已知限制

- 浏览器视觉交互由一次性会话执行，截图不提交；自动化部分不渲染画布，`P3-2`、`P3-3` 的布局只由上述人工复验与单元测试间接保证。
- `P3-2` 对大量同名长文件名的孤立节点簇仍会拥挤：节点间距不随标签宽度自动扩张，仅保证标签被省略上限截断。
- 真实 Dreaming runner 生成、多阶段冲突递进、adapter `REVISION_MISMATCH` 重探测与真实跨进程并发写未在本轮浏览器验证中覆盖，其协议回归沿用既有用例。
