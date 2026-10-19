# 11. 基于 GitHub Actions 的 Dreaming 远端整理方案

> 状态：`G-1`–`G-4` 已实现（2026-10-19，`T082`、`T083`、`T086`）。真实 GitHub Actions 与 DeepSeek 端点已完成一次端到端运行（运行 39226384745）；带实际文件变更的提案应用与其余三家提供商尚未在真实环境验证。本文档定义基于 GitHub Actions 的 Dreaming 远端整理架构、Git 分支触发与回传协议、代码控制的多步结构化调用流程、内置模型提供商配置与验收矩阵。基础模型、写入事务与生命周期规则引用 [02](./02-domain-models-and-contracts.md)、[03](./03-workflows-and-dataflows.md) 与 [10](./10-memory-lifecycle.md)。

## 1. 结论

- 删除本地 CLI runner（`claude`、`codex`）及平台沙箱（macOS Seatbelt）执行路径，Dreaming 统一改为依托记忆仓库所在 GitHub 远端的 GitHub Actions 临时虚拟机执行。
- 本地 `memctl` 不存储、不复制、不传递任何模型 API Key，也不新增云端服务鉴权凭据；触发整理与取回结果完全复用记忆仓库已授权的 Git 远端传输通道（SSH 或 HTTPS credential helper）。
- 模型调用由运行在 GitHub Actions 内的 `memctl dream --worker` 以代码控制的有界多步结构化输出完成，不使用不可控的自主 bash Agent 循环：先由本地解析器与 `doctor` 规则提取静态候选，再按批次调用模型输出严格符合 JSON Schema 的文件变更，每步经代码守卫校验后落盘。
- 内置 4 个模型提供商：`deepseek`、`minimax`、`openai`、`anthropic`。用户在仓库的 GitHub Actions Secrets 中配置对应 API Key，触发时通过 `--runner <provider>`（或 UI 下拉框）指定本次使用的提供商。
- 前置条件：记忆仓库已配置并确认私有 GitHub 远端（`init <url> --confirm-private`），且仓库内已提交 `.github/workflows/memctl-dream.yml` 工作流文件（可通过 `memctl dream --init-workflow` 一键生成并提交）。
- 本地 `memctl` 将远端 Action 产出的结果树视为不可信输入，在本地固定基线上重新执行完整的路径禁区、格式、链接与限额校验，生成 `DreamProposal` 与 `patchHash`。未经用户在本地显式确认（`dream --apply` 或 UI 确认），主分支不会发生任何修改。

## 2. 系统如何工作

### 2.1 端到端数据流

```text
CLI dream --runner <provider>          POST /api/v1/dream:preview
              │                                    │
              └─────────────────┬──────────────────┘
                                ▼
  ① 本地锁内固定基线（fixBaseline）
     校验：存在 MEMORY.md / 无未完成事务与冲突 / 工作树干净
           / 远端已配置并确认私有 / 存在 .github/workflows/memctl-dream.yml
     记录 baseHead + baseRevision，持久化 DreamProposal(status=generating)
     释放主仓库锁
                                ▼
  ② 远端分支触发（复用已授权 Git 凭据）
     git push origin <baseHead>:refs/heads/dream/req/<proposalId>/<provider>
                                ▼
  ③ GitHub Actions 临时虚拟机执行（memctl dream --worker）
     检出 baseHead → 立即删除远端 dream/req/<proposalId>/<provider>
     阶段 A：vault + doctor 静态扫描，识别膨胀索引、断链、孤立文件与过期 until
     阶段 B：按主题分批发起结构化模型调用（JSON Schema + 代码级单步守卫）
     阶段 C：对齐 MEMORY.md 入口索引，以 baseHead 为父提交创建结果提交
             （提交正文附带结构化运行元数据 JSON）
     推送 HEAD:refs/heads/dream/res/<proposalId>（失败时推送空树提交携带错误原因）
                                ▼
  ④ 本地轮询拉取与不可信输入校验
     git fetch origin +refs/heads/dream/res/<proposalId>:refs/memctl/dream/<proposalId>
     校验结果提交的唯一父提交 == baseHead
     若提交元数据标记 status=failed → 记录错误，提案转入 failed
     若 status=ready → 导出结果树到临时目录，对比本地固定基线执行
                       scanWorkTree + buildChanges + protectedPath + linkWarnings + patchHash
     持久化 DreamProposal(status=ready/failed)，清理本地临时目录与远端 dream/res 分支
                                ▼
  ⑤ 本地显式确认与应用（ApplyDream，持主仓库锁）
     校验 proposalId + patchHash + 原始 baseRevision
     基线已变 → stale；工作树脏 → 保持 ready 可重试
     通过受管事务原子写入并提交到主分支 → 记录 appliedCommit → 触发后台同步
```

### 2.2 Git 分支与提交元数据协议

`memctl` 与 GitHub Actions 之间只通过 Git 引用（refs）和提交对象通信，不调用 GitHub REST API，因此不需要在本地配置 GitHub Personal Access Token。

| 要素 | 格式或位置 | 规则 |
| --- | --- | --- |
| 请求引用 | `refs/heads/dream/req/<proposalId>/<provider>` | 直接指向本地固定的 `baseHead` 提交对象，不在本地仓库创建额外提交；`<proposalId>` 为 `dp-<32 位十六进制>`，`<provider>` 为内置提供商标识符 |
| 结果引用 | `refs/heads/dream/res/<proposalId>` | 由 Action 以 `baseHead` 为唯一父提交创建并推送；本地拉取到私有命名空间 `refs/memctl/dream/<proposalId>` 后立即删除远端引用 |
| 结果提交标题 | `dream(<proposalId>): <status>` | `<status>` 取 `ready` 或 `failed` |
| 结果提交正文 | 围栏外的单行或多行 UTF-8 JSON | 承载结构化运行元数据 `DreamWorkerMeta`，供本地读取警告或失败原因 |

`DreamWorkerMeta` 的 JSON 结构如下：

```json
{
  "schemaVersion": 1,
  "proposalId": "dp-0123456789abcdef0123456789abcdef",
  "baseHead": "e4b8...",
  "provider": "deepseek",
  "model": "deepseek-chat",
  "status": "ready",
  "steps": 2,
  "warnings": [
    "notes/prefs.md: 保留了两条互相矛盾的编辑器偏好条目供人工审阅"
  ],
  "error": ""
}
```

协议不变量：

1. 本地 `memctl` 拉取 `refs/memctl/dream/<proposalId>` 后，`MUST` 校验该提交的父提交列表长度恰好为 1 且父提交哈希等于提案记录中的 `baseHead`；不满足时拒绝结果并将提案标为 `failed`，防止远端分支被替换为无关历史。
2. 本地 `memctl` 不信任结果提交中的文件树或 `DreamWorkerMeta.status` 声称的合规性：`MUST` 将结果树导出到独立临时目录后，与本地导出的 `baseHead` 基线逐字节比对，重新运行完整的 `protectedPath`、符号链接、文件模式、UTF-8 文本、最大变更数（`MaxChanges = 100`）与最大字节数（`MaxChangeBytes = 8 MiB`）检查。
3. 当 Action 内发生任何错误（缺少 API Key Secret、模型返回 401/429、超过最大重试次数或产出越界路径）时，工作流的收尾步骤 `MUST` 基于 `baseHead` 创建允许空树的提交（`git commit --allow-empty`），在 `DreamWorkerMeta` 中填入 `status: "failed"` 与脱敏后的 `error`，并推送到 `refs/heads/dream/res/<proposalId>`。本地轮询在下一次 `fetch`（间隔 5 秒）即可立即拿到失败原因并结束等待，无需等到 10 分钟总超时。

### 2.3 代码控制的多步结构化调用流程

`memctl dream --worker` 在 GitHub Actions 运行器内执行，复用 `internal/vault`、`internal/doctor` 与 `internal/dream` 的解析与校验逻辑，按以下三个确定性阶段运行。

#### 阶段 A：静态分析与分批规划（0 次模型调用）

1. 打开工作树根句柄，调用 `vault.ListManaged()` 获取受管记忆文件列表（自动跳过 `.git` 与 `.github`）。
2. 调用 `doctor.Check()` 获取结构化诊断问题，提取与整理相关的四类静态信号：
   - `ENTRY_BLOAT`：`MEMORY.md` 行数超过 100 行阈值；
   - `ORPHAN_FILE`：未被 `MEMORY.md` 可达路径引用的孤立文件；
   - `INVALID_LINK` / `MISSING_LINK` / `AMBIGUOUS_LINK`：死链或同名歧义链接；
   - `MISSING_RECOMMENDED_METADATA`：缺少 `source` 或 `added` 的条目。
3. 扫描全部 Markdown 条目，用确定性规则标记两类候选：
   - **到期候选**：元数据包含 `until: YYYY-MM-DD` 且日期早于当前 UTC 日期的有时效型条目；
   - **重复候选**：规范化文本（去除空白与标点后）完全相同或指向同一主题词的跨文件条目对。
4. **批次切分**：
   - 若受管 Markdown 文件总数 $\le 5$ 且总字节数 $\le 32\text{ KiB}$，归为单批次（1 次主调用完成）；
   - 否则按 `MEMORY.md` 中的一级/二级标题及 `[[path]]` 邻接关系将子文件聚合成最多 `MaxBatches = 5` 个批次，每批次输入正文上限为 `MaxBatchInputBytes = 64 KiB`。超出批次上限的其余无诊断问题文件不在本次整理中发送。

#### 阶段 B：分批结构化改写与代码级单步守卫（1 至 5 次模型调用）

每个批次向选定的模型提供商发起一次非流式结构化请求。

1. **系统提示词（固定契约）**：内嵌 [10. 记忆更新与过期](./10-memory-lifecycle.md#3-记忆分类与处理规则) 的三档分类规则与格式约束：
   - **状态型记忆**（偏好、配置、当前状态、项目现状）：合并完全重复项；同一事实若有新旧两条且新条目有更新的 `added` 日期，就地保留最新事实并移除已失效旧值，不写变更日志；若两条记忆相似但矛盾且无法从日期或上下文判断先后，**保留两条原文及各自元数据**，并在 `warnings` 中说明矛盾点。
   - **事件型记忆**（决策及理由、发布记录、事故排查、架构迁移、带明确历史日期的事项）：**只增不改、禁止删除**；仅允许将其从膨胀的 `MEMORY.md` 拆分或归入对应主题文件，且必须保留原始事实文本、`source` 与 `added` 元数据，并在 `MEMORY.md` 保持 `[[path]]` 链接可达。
   - **有时效型记忆**（带 `until: YYYY-MM-DD`）：仅当静态分析已标记为到期（`until < today`）时允许移除；未到期条目原样保留。
   - **格式与安全边界**：保持每条记忆单行 `- <fact> [source: <tag>; added: YYYY-MM-DD]` 格式；保持根相对 `[[path]]` 链接语法；不虚构事实、不伪造 `source` 标签；只输出符合 Schema 的 JSON 对象。
2. **输入载荷（User Message JSON）**：
   - `today`：当前 UTC 日期（`YYYY-MM-DD`）；
   - `entryFile`：当前 `MEMORY.md` 全文；
   - `batchFiles`：本批次目标文件的 `{path, content}[]`；
   - `staticFindings`：本批次涉及的 `doctor` 诊断项、到期候选与重复候选列表。
3. **输出 Schema（`DreamStepOutput`）**：

```json
{
  "type": "object",
  "required": ["operations", "warnings"],
  "additionalProperties": false,
  "properties": {
    "operations": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["action", "path", "rationale"],
        "additionalProperties": false,
        "properties": {
          "action": { "type": "string", "enum": ["create", "update", "delete"] },
          "path": { "type": "string" },
          "content": { "type": "string" },
          "rationale": { "type": "string" }
        }
      }
    },
    "warnings": {
      "type": "array",
      "items": { "type": "string" }
    }
  }
}
```

4. **代码级单步守卫（Step Guard）**：每次模型返回后，worker 在内存副本上依次执行以下确定性检查：
   - **JSON 与 Schema 校验**：解析 JSON（对不具备原生 `json_schema` 强制解码的提供商先剥离可能存在的首尾 Markdown 代码围栏），校验必填字段、`action` 枚举与额外属性；`create`/`update` 必须提供非空 `content`，`delete` 不得删除 `MEMORY.md`。
   - **路径与范围校验**：所有 `path` 必须通过 `vault` 根相对路径校验且不命中 `protectedPath`（拒绝 `.git`、`.github`、凭据名等）；单步不得修改批次外且非 `MEMORY.md` 的文件。
   - **格式与链接预检**：将 `operations` 应用到内存文件树后，调用 `vault.ParseFile` 校验每个变更文件语法合法，且没有引入新的 `INVALID_LINK` 或 `MISSING_LINK`。
   - **有界重试**：若上述任一检查失败，worker 将具体校验错误追加为一条纠正消息，在同一批次内最多重试 1 次（`MaxStepRetries = 1`）。重试仍失败则中止 worker 并输出失败提交。

#### 阶段 C：入口索引对齐与结果提交（0 或 1 次模型调用）

1. 当阶段 B 共执行了多个批次，且批次间新增、删除了子文件或 `MEMORY.md` 仍存在 `ORPHAN_FILE` / `ENTRY_BLOAT` 时，worker 发起最后 1 次轻量结构化调用：输入当前 `MEMORY.md` 全文与所有受管文件的 `{path, title, headings, lineCount}` 摘要，仅允许输出对 `MEMORY.md` 的 `update` 操作，确保所有主题文件在 `MEMORY.md` Index 中可达且 `MEMORY.md` 行数收敛。单批次运行或索引已完整时跳过此步。
2. 单次 Dreaming 运行的模型调用总次数上限为 `MaxTotalCalls = 12`（含纠正重试），单次 HTTP 调用超时 `120 秒`，worker 总执行预算 `8 分钟`。
3. 全部批次通过后，worker 将内存文件树写回工作目录，生成 `DreamWorkerMeta` 提交并推送到 `refs/heads/dream/res/<proposalId>`。

### 2.4 内置模型提供商

worker 内置以下 4 个模型提供商，全部使用 Go 标准库 `net/http` 直接调用，不引入第三方 SDK 依赖。

| 提供商 ID（`--runner`） | 默认端点 | 必填 Secret 环境变量 | 默认模型 | 结构化输出适配方式 |
| --- | --- | --- | --- | --- |
| `deepseek` | `https://api.deepseek.com/chat/completions` | `DEEPSEEK_API_KEY` | `deepseek-chat` | OpenAI 兼容请求 + `response_format: {"type": "json_object"}` + 提示词内嵌 Schema + Step Guard 校验 |
| `minimax` | `https://api.minimaxi.com/v1/chat/completions` | `MINIMAX_API_KEY` | `MiniMax-M2.7` | OpenAI 兼容非流式请求（`stream: false`）+ 提示词内嵌 Schema + 围栏剥离与 Step Guard 校验 |
| `openai` | `https://api.openai.com/v1/chat/completions` | `OPENAI_API_KEY` | `gpt-4.1` | OpenAI 兼容请求 + `response_format: {"type": "json_schema", "json_schema": {..., "strict": true}}` + Step Guard 校验 |
| `anthropic` | `https://api.anthropic.com/v1/messages` | `ANTHROPIC_API_KEY` | `claude-sonnet-4-5` | Anthropic Messages 请求（`anthropic-version: 2023-06-01`）+ 单工具定义强制结构化输入（`tool_choice: {"type": "tool", "name": "emit_dream_step"}`）+ Step Guard 校验 |

可选覆盖变量（配置在 GitHub 仓库的 Actions Variables 中，无需修改工作流文件）：

- `MEMCTL_DREAM_MODEL`：覆盖所选提供商的默认模型名称。
- `MEMCTL_DREAM_BASE_URL`：覆盖默认 API 基础地址（例如将 `minimax` 切换为国际站 `https://api.minimax.io/v1/chat/completions`，或指向兼容网关）。

调研依据：

- DeepSeek 官方 `/chat/completions` 支持 `response_format: {"type": "json_object"}`（要求提示词显式出现 `json` 字样并设置充足 `max_tokens`），不支持 `type: "json_schema"`（传入返回 HTTP 400）。
- MiniMax M2.5/M2.7 系列在 OpenAI 兼容端点上不保证强制执行 `json_schema`，且 `response_format` 与 `stream: true` 互斥；因此统一使用 `stream: false` 并在 Step Guard 中强制校验。
- Anthropic Messages API 支持通过 `tools` + `tool_choice` 强制模型生成符合 JSON Schema 的结构化参数，兼容所有支持工具调用的 Claude 3.5/4.x 模型，无需依赖特定 beta header。

### 2.5 工作流文件与自部署配置

记忆仓库中的工作流路径固定为 `.github/workflows/memctl-dream.yml`。`memctl` 二进制在 `internal/assets` 内嵌该模板，可通过 `memctl dream --init-workflow` 写入记忆仓库并提交；模板中的版本占位符会被替换为执行安装的 `memctl` 版本，使工作流下载与当前二进制一致的 Linux 发布产物（开发构建不替换，保留占位符以便发现）。

工作流模板核心结构如下：

```yaml
name: memctl-dream

on:
  push:
    branches:
      - 'dream/req/**'

permissions:
  contents: write

concurrency:
  group: memctl-dream-${{ github.ref_name }}
  cancel-in-progress: false

jobs:
  dream:
    runs-on: ubuntu-latest
    timeout-minutes: 9
    env:
      GITHUB_REF_NAME: ${{ github.ref_name }}
      GITHUB_SHA: ${{ github.sha }}
      MEMCTL_DREAM_VERSION: <安装工作流时的 memctl 版本>
      MEMCTL_DREAM_MODEL: ${{ vars.MEMCTL_DREAM_MODEL }}
      MEMCTL_DREAM_BASE_URL: ${{ vars.MEMCTL_DREAM_BASE_URL }}
      DEEPSEEK_API_KEY: ${{ secrets.DEEPSEEK_API_KEY }}
      MINIMAX_API_KEY: ${{ secrets.MINIMAX_API_KEY }}
      OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
      ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
    steps:
      - name: Checkout baseline
        uses: actions/checkout@v4
        with:
          fetch-depth: 1

      - name: Delete request branch
        run: |
          git push origin --delete "${GITHUB_REF_NAME}" || true

      - name: Install memctl Linux binary
        run: |
          BASE="https://github.com/gray0128/memctl/releases/download/${MEMCTL_DREAM_VERSION}"
          # SHA256SUMS lists the release asset name, so the download keeps that
          # name for `sha256sum -c` to find it.
          mkdir -p /tmp/memctl-dream
          cd /tmp/memctl-dream
          curl -fsSL "${BASE}/memctl-linux-amd64" -o memctl-linux-amd64
          curl -fsSL "${BASE}/SHA256SUMS" -o SHA256SUMS
          grep "  memctl-linux-amd64$" SHA256SUMS | sha256sum -c -
          chmod +x memctl-linux-amd64

      - name: Run Dreaming worker
        run: |
          /tmp/memctl-dream/memctl-linux-amd64 dream --worker \
            --ref "${GITHUB_REF_NAME}" \
            --base-head "${GITHUB_SHA}"
```

`memctl dream --worker` 内部自带异常捕获与最终推送逻辑：无论整理成功或中途报错，都会在工作树配置临时提交身份（`memctl-dream[bot]`），基于 `--base-head` 创建结果提交并执行 `git push origin HEAD:refs/heads/dream/res/<proposalId>`，随后以退出码 `0`（已成功回传错误报告）或非零（连推送都失败）结束。

用户自部署只需三步：

1. 确保记忆仓库已绑定私有 GitHub 远端：`memctl init git@github.com:<user>/<repo>.git --confirm-private`。
2. 在该 GitHub 仓库的 `Settings → Secrets and variables → Actions` 中添加任一提供商的 Secret（如 `DEEPSEEK_API_KEY`）。
3. 运行 `memctl dream --init-workflow` 生成并推送 `.github/workflows/memctl-dream.yml`，之后即可在任意设备通过 `memctl dream --runner deepseek` 或 Web UI 触发整理。

## 3. 安全、治理与禁止项

### 3.1 信任边界

| 边界 | 机制与保障 |
| --- | --- |
| 本地不接触模型凭据 | API Key 仅存放在 GitHub Actions Secrets，只在 GitHub 临时运行器内存中注入给 `memctl dream --worker`；本地 `memctl` 遵循 [凭据与信任边界](./01-architecture-and-modules.md#34-凭据与信任边界)，不读取、不存储、不传输任何模型密钥 |
| 无死循环与工作流篡改 | GitHub Actions 平台保证由默认 `GITHUB_TOKEN` 推送的 `dream/res/<proposalId>` 分支不会触发新的工作流运行，且 `GITHUB_TOKEN` 被平台禁止修改 `.github/workflows/` 下的任何文件 |
| 本地路径禁区兜底 | 本地 `vault` 与 `dream` 将 `.github` 与 `.git` 同列为最高优先级受保护目录：即使远端被恶意篡改并推送了包含 `.github/workflows/` 修改的结果分支，本地 `scanWorkTree` 与 `buildChanges` 也会立即拒绝该提案（`D03`），防止在本地 `ApplyDream` 时用用户高权限凭据提交工作流改动 |
| 确认前不改主分支 | Action 只写 `refs/heads/dream/res/<proposalId>`，不写 `main`；本地 `ApplyDream` 校验 `proposalId + patchHash + baseRevision` 通过后才在本地受管事务中提交到主分支 |
| 错误与日志脱敏 | `DreamWorkerMeta.error` 与提案 `error` 字段在写入前剥离任何 Bearer Token、`sk-` 前缀密钥及未脱敏 URL |

### 3.2 禁止项

1. **禁止本地直接调用模型或运行本地 Agent CLI**：`memctl dream`（非 `--worker` 模式）不得在用户机器上启动本地模型 CLI 或直接向模型 API 发起请求，否则破坏「本地零模型凭据」与跨平台一致性不变量。
2. **禁止常规读写与提案触碰 `.github/`**：`vault.ListManaged()`、`read`、`write`、`mcp`、UI 编辑与 Dreaming 提案对比 `MUST` 忽略或拒绝 `.github` 及其大小写等价路径；仅 `memctl dream --init-workflow` 允许在仓库锁内创建或更新 `.github/workflows/memctl-dream.yml`，违反时返回 `INVALID_PATH` 或拒绝提案。
3. **禁止跳过父提交与基线重算校验**：本地拉取 `dream/res/<proposalId>` 后，不得直接执行 `git merge` 或 `git cherry-pick`；`MUST` 校验父提交等于 `baseHead`，并将结果树作为不可信文件集重算 Diff 与 `patchHash`，否则恶意或过期的远端分支可绕过受管写入事务。
4. **禁止保留已消费的远端临时分支**：提案进入 `ready` 或 `failed` 终态、或本地轮询超时取消后，本地 `memctl` `MUST` 尽力删除远端 `refs/heads/dream/req/<proposalId>/*` 与 `refs/heads/dream/res/<proposalId>` 及本地 `refs/memctl/dream/<proposalId>`，防止远端分支堆积。

## 4. 历史依据：为什么弃用本地 Runner 与 Cloudflare Worker

### 4.1 弃用本地 CLI Runner 的依据

2026-10-19 在 macOS arm64 上对本机 4 个候选 CLI（`claude`、`codex`、`pi`、`dsh`）进行了实机隔离验证（完整记录见 [Dreaming 交接文档](./validation/dreaming-handoff.md#9-真实-runner-验证结果关键阻塞)）：

- Seatbelt 沙箱要求隔离 `HOME` 并阻止写用户配置，而 `claude` 依赖 macOS Keychain 凭据、`dsh` 依赖宿主进程内部凭据服务，在隔离环境下均因无法认证而失败；
- 暂存静态凭据文件（方案 A）违反 [01 架构契约](./01-architecture-and-modules.md#34-凭据与信任边界)「`memctl` 不存储、不复制、不生成凭据」的硬性规定，且对含 `refresh_token` 轮换的 OAuth 凭据（如 `codex`）会导致宿主掉登录；
- 本地沙箱强绑定 macOS Seatbelt，使 Linux 与 Windows 平台长期处于 `UNSUPPORTED`；
- 本地 Agent CLI（如 `pi`）会加载宿主已安装的 `memctl-pi` 扩展，在整理期间重入触发 `memctl start/sync` 争抢仓库锁。

### 4.2 选定 GitHub Actions 而非 Cloudflare Worker 的依据

| 评估维度 | GitHub Actions（选定） | Cloudflare Worker（未采用） |
| --- | --- | --- |
| 本地凭据契约兼容性 | 完全兼容：复用已有的 Git SSH/HTTPS 远端同步凭据，本地零新增凭据 | 冲突：本地必须存储并发送调用 Worker 的鉴权 Token |
| 数据接收方 | 不新增第三方基础设施（同步用户的记忆本来就托管在私有 GitHub 仓库），仅发往所选模型提供商 | 新增 Cloudflare 作为全量明文记忆的中间接收方 |
| 执行时长与多步处理 | 单次运行预算充裕（默认 9 分钟超时），可直接运行同一套 Go 二进制复用 `vault` 与 `doctor` | 受 HTTP 请求超时与 CPU 时间限制，多步长耗时调用需额外引入 Workflows 或 Durable Objects |
| 其他用户自部署成本 | 在既有记忆仓库添加 1 个工作流文件 + 1 个 GitHub Secret | 需注册 Cloudflare、安装 `wrangler` 部署服务、配置 Worker Secret 并在每台设备配置客户端 Token |

## 5. 契约改动映射

本方案对既有契约文档（`01`–`05`）的修订点汇总如下：

| 文档 | 章节 | 修订内容 |
| --- | --- | --- |
| [01. 架构与模块](./01-architecture-and-modules.md) | 3.1、3.2、3.4、4、5 | `.github` 纳入非记忆受保护路径；删除本地 runner 沙箱依赖；说明模型 API Key 仅存于 GitHub Actions Secrets；`internal/dream` 职责更新为远端分支触发、worker 多步结构化整理与结果树校验 |
| [02. 模型与契约](./02-domain-models-and-contracts.md) | 2.3、3、4.2 | `DreamProposal.runner` 与 `POST /api/v1/dream:preview` 的 `runner` 取值改为 `"deepseek" \| "minimax" \| "openai" \| "anthropic"` |
| [03. 流程与恢复](./03-workflows-and-dataflows.md) | 6 | 七步流程改为：锁内校验基线与工作流文件 → 推送 `dream/req` 分支触发 Action → worker 静态候选识别与多步结构化调用并推送 `dream/res` 分支 → 本地拉取结果树并按不可信输入计算 Diff 与 `patchHash` → 持久化提案并清理远端临时分支 → 确认后受管事务应用 |
| [04. CLI、UI 与分发](./04-ui-and-cli-specification.md) | 1、4、5、6 | `dream` 命令支持 `--runner deepseek\|minimax\|openai\|anthropic`、`--init-workflow` 与内部 `--worker`；UI「整理接入」展示提供商选择与工作流就绪状态；运行时不再依赖本地 runner 与平台沙箱 |
| [05. 验收与交付](./05-validation-and-delivery.md) | 1、2、3 | 更新 `D01`–`D04` 与 `R02` 验收口径，消除非 macOS 平台在 Dreaming 上的 `UNSUPPORTED` 限制，新增阶段 G 实施计划 |

## 6. 验收矩阵与实施计划

### 6.1 验收场景（更新 `D01`–`D04` 与子场景）

| ID | 输入或故障场景 | 可验证结果 |
| --- | --- | --- |
| `D01` | 未配置远端、远端未确认私有、缺少 `.github/workflows/memctl-dream.yml`、未知 `runner` | 拒绝生成并返回针对性错误（`UNSUPPORTED` 或 `REMOTE_NOT_APPROVED`）；不推送分支，不修改主库或用户配置 |
| `D02` | 未提交内容、生成期间主库有新提交、应用前内容改变 | 脏库拒绝生成；旧基线提案拒绝应用并标为 `stale` |
| `D03` | 结果分支父提交不匹配 `baseHead`，或包含删除 `MEMORY.md`、修改 `.github/**`/`.git/**`、路径逃逸、符号链接、超限文件 | 本地范围与父提交检查拒绝该结果，提案标为 `failed`，不修改主库，远端临时分支被清理 |
| `D04` | 同一提案连续 apply、应用中断后重试、`--init-workflow` 幂等初始化 | apply 返回同一提交或恢复状态，不重复提交；`--init-workflow` 在干净仓库写入并提交工作流文件，已有最新文件时不产生空提交 |
| `D05` | `dream --worker` 对接模拟模型端点：静态候选提取、多步分批、非法 JSON 自动纠正重试、API 失败空提交回传、事件型条目保护 | 正确发送静态诊断与批次文件；单次非法 JSON 触发 1 次纠正重试后成功；API 401/500 时推送 `status: "failed"` 空提交且密钥脱敏；本地轮询立即感知失败 |

### 6.2 阶段 G 实施顺序

| 步骤 | 前置依赖 | 本轮交付边界 | 对应验收 |
| --- | --- | --- | --- |
| `G-1`：方案与契约修订（`T082`，本轮） | `T081` | 完成 `docs/11` 方案文档，同步修订 `docs/01`–`05`、`docs/README.md` 与 `docs/validation/dreaming-handoff.md` | 文档链接、锚点与跨文档契约一致性检查 |
| `G-2`：`.github` 边界、Worker 与远端分支编排 | `G-1` | `internal/vault` 与 `internal/dream` 将 `.github` 纳入保护；实现 `internal/dream` 的 4 个提供商适配器、三阶段多步结构化整理 worker、Git `dream/req` 与 `dream/res` 分支编排；删除 Seatbelt 沙箱与本地 CLI runner 代码 | `D01`–`D05` 单元测试与本地受控 Git 远端 + 模拟模型端点集成测试 |
| `G-3`：CLI `--init-workflow`、帮助与 Web UI 适配 | `G-2` | 实现 `memctl dream --init-workflow` 与内嵌工作流模板，更新 CLI 帮助（中英与 JSON）、`server` 探针与 `DreamPanel.jsx` 提供商选择器 | `D04`、`R06`、UI 构建与 HTTP 预览/应用集成测试 |
| `G-4`：真实 GitHub Actions 端到端验证 | `G-3`；配置好 Secret 的私有 GitHub 记忆仓库 | 在真实私有 GitHub 记忆仓库触发 Actions 运行，完成生成、拉取、Diff 审阅与 `apply` 全链路验证并归档证据 | `D01`–`D05` 真实远端与模型端点验收 |
