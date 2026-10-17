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

## 5. 第二轮验证（v0.1.1 发布后复验）

2026-10-17 对 v0.1.1 发布候选二进制执行第二轮 ego-browser 端到端验证，用于确认 T040 界面重设计与 T042 适配器修复之后，七个视图与主要写路径没有视觉或交互回归。

### 5.1 环境与步骤

- 二进制：`dist/memctl-darwin-arm64`（v0.1.1，内嵌当轮 `web/dist`）；ego-browser v2.1.0。
- 仓库：临时 HOME 与临时记忆仓库，包含 5 个文件、11 条事实、1 个死链与 1 个孤立文件；读写服务 `127.0.0.1:18420`，只读服务 `127.0.0.1:18421`（`--readonly`）。
- 冲突场景：本地裸远端加 peer 克隆，两端修改同一行后由 UI「立即同步」捕获冲突，再在「冲突解决」提交合并结果。
- 覆盖：概览、知识图谱、条目、冲突解决、审计、诊断、整理接入七个视图；条目筛选与检索、条目编辑与 412 草稿差异、文件原文编辑、审计 Diff 展开与回滚、手动同步（无上游与有上游）、adapter 计划预览、只读模式、深色模式、390 px 窄屏。

### 5.2 通过项

- 七个视图均渲染完成，视图遍历期间未捕获浏览器 `error` 或 `unhandledrejection`。
- 条目检索、条目编辑提交（`fd9c2db`）与外部修改后的 412 草稿保留符合契约；冲突提示并列显示服务器当前行与草稿差异。
- 文件原文编辑提交（`116d9ff`）成功；审计回滚生成逆向提交（`fcb5fd9`）。
- 冲突三方视图（BASE/ONTO/REPLAYED）与提交解决可用；解决后顶栏显示 `synced`、`+0 / -0`。
- 无上游时手动同步显示「领先 未知 · 落后 未知」，与顶栏一致。
- 只读服务禁用同步、条目编辑、adapter 预览与 Dreaming 生成。
- 深色模式与 390 px 窄屏无横向溢出。

### 5.3 发现的问题

| ID | 级别 | 现象 | 根因与修复方向 |
| --- | --- | --- | --- |
| `R2-P2-1` | P2 | 审计 Diff 每行左端被裁掉约 7.5em：`# Web UI notes` 显示为 `otes`，`2026-10-14` 显示为 `14:` | `web/src/styles.css` 的 `.diff-preview .d2h-code-line { padding: 2px 8px }` 覆盖了 diff2html 为绝对定位行号列预留的左内边距；改为 `padding: 2px 8em` 后实测恢复 |
| `R2-P2-2` | P2 | 同步消息横幅显示时滚动页面，导航栏滑入固定头部下方且无法点击 | `TopBar` 把同步消息放进 sticky header，header 高度从 57 px 增至 103 px；`nav` 仍是 `sticky top-[57px]`、`z-20`，被 `z-30` 的 header 覆盖 |
| `R2-P3-1` | P3 | 冲突解决成功后，顶部仍保留「sync stopped on conflict … run memctl resolve」红色横幅 | `syncMessage` 只在下一次同步时覆盖，冲突解决仅刷新快照 |
| `R2-P3-2` | P3 | 仓库锁被占用时，条目与审计视图直接显示含绝对路径的 `repository lock … is held by another process`，不重试；锁释放后重新查询才恢复 | 读取失败没有有界重试；快照读取有 `loadFreshSnapshot` 重试，其他读取没有 |
| `R2-P3-3` | P3 | 顶栏「最近同步」显示原始 UTC 时间串（`2026-10-17T09:39:32Z`） | 直接渲染 `lastSuccessAt`，未按本地时区格式化 |
| `R2-P3-4` | P3 | 知识图谱标签在部分布局下仍互相压盖（`cli-release-e2e.md` 与 `MEMORY.md`、`missing-file` 与长文件名） | 沿用 `T039` 的已知限制：cose 布局未按标签宽度扩张间距；重新布局后消失 |

以上问题已由 `T044` 收口，复验见第 6 节。

### 5.4 未覆盖

- 图谱节点拆分：画布节点难以稳定命中，本轮只验证图谱渲染与布局，拆分流程沿用自动化测试。
- Dreaming 真实 runner 生成、多阶段冲突递进、adapter 安装应用与 `REVISION_MISMATCH` 重探测。
- 非 macOS arm64 平台。

截图保存在会话临时目录，未提交；长期可复核证据仍为第 3 节与 `STATUS.md` 指向的自动化回归。

## 6. 第二轮问题的收口与复验（T044）

按第二轮验证的 6 项问题修复，代码变更只涉及 `web/src`，并在 [Web UI](../04-ui-and-cli-specification.md#4-web-ui) 补充时间格式与 `BUSY` 重试说明；不改 REST 契约、状态字段或写入流程。

- `R2-P2-1`：`.diff-preview .d2h-code-line` 恢复 8em 水平内边距。复验：Diff 文本左边界 x=426，行号列右边界 x=414，文本不再被行号列覆盖。
- `R2-P2-2`：`TopBar` 用 `ResizeObserver` 把实际 header 高度写入 `--memctl-header-height`，导航栏改为 `sticky top-[var(--memctl-header-height,57px)]`。复验：横幅显示且滚动 500 px 后 header 占 0–103、导航占 103–148，导航项命中测试返回按钮内 span；390 px 下 header 234、导航 top 234，无横向溢出。
- `R2-P3-1`：`refreshSnapshot` 清除旧的 `syncMessage`。复验：冲突解决成功后顶部不再显示「sync stopped on conflict」横幅，顶栏显示 `synced`。
- `R2-P3-2`：新增 `web/src/retry.js`，GET 读取遇 `BUSY` 做 4 次有界指数退避；耗尽后返回「仓库正忙（可能正在同步），请稍后重试」。复验：锁被占用 0.6 秒时查询无错误；占用 9 秒时约 1.4 秒后显示友好提示且不泄漏绝对路径，锁释放后重新查询恢复。
- `R2-P3-3`：新增 `web/src/time.js`，顶栏「最近同步」按本地时区显示 `YYYY-MM-DD HH:mm`。复验：显示 `2026-10-17 17:54`。
- `R2-P3-4`：知识图谱提高节点斥力与间距（`nodeRepulsion: 400000`、`idealEdgeLength: 120`、`nodeOverlap: 120`、`componentSpacing: 200`），标签保持文件名截断（`text-max-width: 120`）。复验：1784 px 下连续 3 次重新布局未出现标签互相压盖；更密集的图谱仍可能拥挤，属已知限制。

回归：

- `cd web && npx vitest run`：16 个文件 85 项通过（新增 `retry.test.js` 3 项、`time.test.js` 3 项）。
- `npm run build` 成功；`go build -o bin/memctl ./cmd/memctl` 成功；`gofmt -l cmd internal tests web/embed.go` 无输出；`go vet ./internal/... ./cmd/...` 通过。
- ego-browser 回归覆盖审计 Diff、同步横幅下的粘性导航、冲突解决横幅、BUSY 重试、时间格式、图谱布局、只读模式、深色模式与 390 px 窄屏。未重跑 `go test ./...`（本轮无 Go 代码变更）。

## 7. 概览对齐、指标卡跳转与来源提示（T045）

用户报告的 4 项界面问题在本节收口，变更只涉及 `web/src` 与 [Web UI](../04-ui-and-cli-specification.md#4-web-ui) 的说明。

- 概览导航与正文未对齐：导航内容容器在 `max-w-7xl` 外先加内边距，主区在 `max-w-7xl` 内加内边距，1315 px 视口下导航首项比 `h1` 左移 17.5 px。把内边距移入导航的滚动容器后，1100/1315/1440/1784 px 下两者左边界分别为 24/41.5/104/276，390 px 下均为 16。
- 选中项左边框被裁切：导航滚动容器原先没有内边距，选中项的 1 px ring 落在内容盒外，被 `overflow-x-auto` 裁掉。容器增加与主区一致的内边距后 ring 完整显示。
- 指标卡跳转：概览四张资产卡与三张诊断卡改为按钮语义（`role="button"`、`tabIndex=0`、回车或空格触发）。管理文件与原子条目跳转条目视图，图谱节点与图谱关系跳转知识图谱，诊断三项跳转诊断视图。复验：点击「原子条目」「图谱节点」「错误诊断」后分别渲染「记忆条目检索」「知识拓扑图谱」「健康诊断与自愈」。
- 来源 URI 完整提示：条目视图的来源 chip 增加 `title`，悬停显示完整 URI（如 `agent://MacBookPro.lan/claude/e2e-s1`），列表仍保持截断显示。

回归：`npx vitest run` 16 文件 85 项通过；`npm run build` 与 `go build` 通过；ego-browser 在 1100/1315/1440/1784 与 390 px 下检查对齐、无横向溢出与选中项边框。截图保存在会话临时目录，未提交。
