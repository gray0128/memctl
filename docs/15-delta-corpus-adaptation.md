# 15. Delta 会话蒸馏适配方案

> 状态：方案已定义（2026-10-31，Delta 0.23.0 / darwin arm64），未实现。本文在 [14. Delta cwd 可行性](./14-delta-cwd-feasibility.md) 的「可行」结论之上给出可实现的契约：读取路径、项目归属、回合与限额、触发时机、失败处置与验收。[14](./14-delta-cwd-feasibility.md) 回答能不能做，本文回答怎么做，以及本机实测推翻了其中哪三条判断。宿主接入面见 [13](./13-delta-integration.md)；蒸馏器的既有契约见 [12 §2.2](./12-session-corpus-dreaming.md#22-本地会话采集与确定性蒸馏internalcorpus)。

## 1. 结论

- **读取路径改用 `delta thread watch <id> --json`**，不再用 `delta thread show --format messages` 分页。watch 输出结构化 JSONL，最终正文与思考段分属不同事件，因此 [14](./14-delta-cwd-feasibility.md) 遗留的两项成本（20 条分页上限、Thinking 混入正文）都不存在。
- **当前线程可直接读取**。设置 `DELTA_CURRENT_THREAD_ID` 时 watch 正常工作，[14 §6](./14-delta-cwd-feasibility.md#6-风险与未决问题) 里「须 `env -u DELTA_CURRENT_THREAD_ID`」的风险不成立；那是 `thread show` 的边界行为，不是 Delta 的限制。
- **项目归属按 [14](./14-delta-cwd-feasibility.md) 第一步执行**：新增 `corpus.ProjectRoot(cwd)`，把进程 cwd 归一化到项目根。Delta 与其它宿主共用同一条规则，对非 Delta 宿主是恒等变换。
- **只采集当前线程，不做全机扫描**。Delta 的 CLI 不提供「线程 → 项目」的映射（§3.3），按项目筛选历史线程无法在不碰 Delta 私有状态的前提下实现。历史线程不进入候选池，这是有意的边界：宁可不采，也不把别的项目的会话塞进当前项目的整理上下文。
- **只在 `dream --prepare` / `memory_dream_prepare` 采集，不进 `sync` / `end`**。watch 到 `live` 标记的固定开销是 1.65–1.93 秒，远超实时采集预算 `LiveBudget = 200 ms`；Delta 也没有 `end` 事件。
- **复用既有解析契约**。watch 流本身就是 JSONL，`Corpus.Collect(agent, sid, reader)` 原样可用，清洗、脱敏、可疑指令过滤、回合裁剪、缓存写入与消费写回都不新增实现。

判据：Delta 适配是否实现，看「`dream --prepare` 在 Delta 线程里能否产出一份 `agent: delta` 的摘要，其 `cwdKey` 与同一项目其它宿主的摘要相同」，而不是看是否存在 Delta 专用采集器。

## 2. 读取路径

### 2.1 两条候选路径的实测对照

测量环境：Delta 0.23.0，darwin/arm64，2026-10-31；样本为本机 8 个 Delta 线程（其中 7 个有用户消息）。本次调研就在其中一个线程里进行，该线程的条数与体积在调研期间持续增长，所以下文涉及它的数字只作量级参考，其余线程为固定值。

| 指标 | `thread show --format messages` 分页 | `thread watch --json` |
| --- | --- | --- |
| 单次可读消息数 | 最多 20 条（`--messages 1-25` 报 `message range must contain 1 to 20`） | 一次读全，无分页 |
| 总数获取 | 无字段；靠 `1-20` 的越界报错反推（`message range 1-20 is outside this thread (14 messages)`） | `ready` 行给出 `thread_id`，消息随流到达 |
| 单次延迟 | 0.01–0.62 秒 | 1.65–1.93 秒到 `live` |
| 产出体积 | 线程 3 为 2.48 MB、线程 5 为 4.08 MB（14 条消息） | 线程 3 为 2.11 MB；线程 5 从 6.2 MB 一路增长到 7.0 MB（本次调研就在该线程内进行） |
| 正文质量 | `buffer` 为渲染后整体，以 `Thought N times…` 开头并紧跟 `Thinking` 段 | `agent_message` 是纯正文，`agent_thought` 单独成事件 |
| 当前线程 | 按 id 传入返回 `[]`（退出码 0），是静默空结果 | 正常返回 |
| 进程生命周期 | 调用即退出 | 读到 `live` 后仍常驻，必须主动终止 |

判据落在后两列：分页路径需要「分页器 + 试错读总数 + Thinking 剥离 + 当前线程绕行」四项新增实现，watch 路径只需要一个带字节上限与超时的子进程封装。

### 2.2 watch 的实测行为

输出的每一行是一个 JSON 对象，共三类：

```text
{"type":"ready","thread_id":"ksQQUZZp…","producer":null}
{"type":"item","item":{"id":"…","kind":"user_message","after_id":null,"turn_id":null,
  "author":{"user_id":"user_…","name":"gray0128","actor_id":"…"},
  "created_at":1793421165010,"open":false,"content":{"text":"…"}}}
{"type":"live"}
```

`item.kind` 的取值见下表。计数取自本机 8 个线程的一次完整扫描：含当前线程时 `user_message` 37、`agent_message` 157、`agent_thought` 395、`tool_call` 1183、`external_edits` 39、`compaction` 1；仅统计其余 7 个固定线程时为 798 条 item（`user_message` 30、`agent_message` 96、`agent_thought` 89、`tool_call` 580、`external_edits` 3、无 `compaction`）。两组的差值全部来自当前线程，与下文「该线程持续增长」的说明一致。

| `kind` | `content` 结构 | 对蒸馏的含义 |
| --- | --- | --- |
| `user_message` | `{"text":"…"}` | 用户键入的正文，开一个新回合 |
| `agent_message` | `{"text":"…"}` | 最终正文，**不含思考段**，可整段保留 |
| `agent_thought` | `{"text":"…"}` | 模型推理，按 [12 §2.2.4](./12-session-corpus-dreaming.md#4-本地确定性清洗与脱敏规则) 丢弃 |
| `tool_call` | `{toolCallId,name,title,kind,status,content,rawInput,rawOutput}` | 只取名称、`title`、`status` 与 `rawInput` 的命令或路径；`rawOutput` 与 `content` 丢弃 |
| `external_edits` | `{"text":"…"}`（Markdown Diff） | 丢弃：属于工具输出，且单条可达数十 KB |
| `compaction` | `{"text":"…"}`（上下文摘要全文） | 丢弃：是宿主生成的摘要，不是本轮对话 |

三条必须写进契约的观测：

1. **同一回合内 `created_at` 完全相同**（实测同一回合的 4 条 item 时间戳均为 `1793429722860`）。因此回合划分只能按流顺序与 `user_message` 边界，不能按时间戳排序。
2. **`turn_id` 在同一线程内是常量**（实测 8 个线程的 agent 侧 item 共用同一个 `turn_id`，仅 `user_message` 的 `turn_id` 为 `null`）。它不能当回合标识用，只能当「这是 agent 侧事件」的标记。
3. **`live` 之后连接不关闭**：线程 0 在 1.85 秒到达 `live` 后再等 5 秒进程仍未退出。采集必须读到自己需要的标记或达到上限后终止子进程，不能依赖进程自然结束。

另有一项对 §4.3 规则的关键实证：扫描 7 个固定线程的全部 `agent_message` 共 96 条，**没有一条以 `Thought` 开头或包含独立的 `Thinking` 行**；其中有含 `omitted` 字样的条目，逐条核对后均为对话正文在讨论「字段省略」或模型降级文案，不是渲染截断标记。这条结论是「不必在解析器里剥离思考段」的直接依据。

### 2.3 不采用的读取路径

| 路径 | 不采用的理由 |
| --- | --- |
| `show --format messages` 分页 | 需分页器与试错读总数；`buffer` 混入思考段，而 `MaxTurnTextBytes = 1024` 若截断点落在思考段会丢掉整段正文 |
| `show --format summarize` | 输出被硬性截到 24 KiB 并插入 `[... omitted ...]` 占位（线程 5 有 13 处、线程 3 有 3 处），且占位会整段吃掉 agent 段落；本机 8 个线程中有 3 个的 `USER`/`AGENT` 段落序列与 `messages` 数组的角色序列不一致，无法可靠归属到回合 |
| `show --format full` | 单线程 0.25–2.63 MB，且 `messages` 只有 `start_byte`/`end_byte` 偏移，正文要靠切 `buffer` 再剥离思考段 |
| 直读 DeltaDB | 官方要求只用 CLI，即使 SQLite 里字段更全 |
| 在解析器里剥离 `Thinking` | watch 已经把两者分开；为已消失的问题写正则，等于为某个版本的渲染格式留下隐形依赖 |

## 3. 项目归属

### 3.1 `corpus.ProjectRoot(cwd)`

一条命令、两条截断规则：

```text
git -C "$cwd" rev-parse --path-format=absolute --git-common-dir --show-toplevel
```

`--path-format=absolute` 不能省：在普通仓库的子目录里执行时，`--git-common-dir` 会返回相对路径 `../.git`，带不带前缀都不命中截断规则。

| 观测到的形态 | 处置 |
| --- | --- |
| `<repo>/.delta/clones/<id>/<repo>.git` | 截断到 `/.delta/clones/` 之前 |
| `<repo>/.delta/worktrees/<id>/<repo>` | 截断到 `/.delta/worktrees/` 之前 |
| 两条都不命中（普通仓库、子目录、非 Delta 工作树） | 返回入参 |
| 命令失败（非 Git 目录、无 `git` 可执行文件） | 返回入参 |
| 截断后的候选路径不存在 | 返回入参（防止仓库名里恰好包含 `.delta/worktrees/` 这类字面量） |

`--git-common-dir` 规则排在前面，因为它是 Delta 线程 checkout 的实际形态；`--show-toplevel` 规则是独立的第二条路径，两者在 4 个真实 checkout 上给出同一结果。

单次调用取两个值，实测 0.01–0.02 秒。

| 样本 | `--git-common-dir` | `--show-toplevel` | 归一化结果 |
| --- | --- | --- | --- |
| 原仓库 | `.git` | `…/github/memctl` | `…/github/memctl`（恒等） |
| 原仓库子目录 `docs` | `../.git` 或绝对 `…/github/memctl/.git` | `…/github/memctl` | `…/github/memctl/docs`（恒等） |
| 线程 `vt74q90bpvrv` | `…/.delta/clones/vt74q90bpvrv/memctl.git` | `…/.delta/worktrees/vt74q90bpvrv/memctl` | `…/github/memctl` |
| 线程 `eh1c2q2xgcqy` | `…/.delta/clones/eh1c2q2xgcqy/memctl.git` | `…/.delta/worktrees/eh1c2q2xgcqy/memctl` | `…/github/memctl` |
| 线程 `vdrckw4etm5k`（`task-box`） | `…/.delta/clones/vdrckw4etm5k/task-box.git` | `…/.delta/worktrees/vdrckw4etm5k/task-box` | `…/cnb/task-box` |
| 非 Git 目录 `/tmp` | 命令返回 128 | — | `/tmp`（恒等） |

归一化后的键与既有 digest 一致：[14 §3.2](./14-delta-cwd-feasibility.md#32-归一化到项目根后收敛) 已验证 `sha256(…/github/memctl)` 与 57 条既有记录的 `cwdKey` 逐字节相同。

### 3.2 调用点

`ProjectRoot` 只作用在**由进程 cwd 推导项目键**的入口，不作用于日志自带的 `cwd`：

| 入口 | 现状 | 改动 |
| --- | --- | --- |
| `dream --prepare` / `memory_dream_prepare` | `PrepareLocal` 用 `os.Getwd()` 或 `req.Cwd` 直接算 `CwdKey` | 先过 `ProjectRoot` |
| `sync` / `end` 的实时采集 | 日志 `cwd` 优先，缺失时用进程 cwd | 回退值先过 `ProjectRoot`；日志自带 `cwd` 保持原样 |
| `corpus.Backfill` | 用 `req.Cwd` 算 `CwdKey` 并预过滤目录名 | 入参由 `PrepareLocal` 归一化后的值提供，自身不再改 |

副作用边界：日志自带 `cwd` 的宿主（`claude-code`、`codex`、`pi`、`dsh`、`codebuddy`、`workbuddy`、`antigravity`）行为不变，因为它们的日志记的就是项目根；只有「日志没有 `cwd`、退回进程 cwd」这条路径会看到归一化结果。

### 3.3 为什么不做「线程 → 项目」索引

要让历史线程按项目筛选，得先知道每个线程干在哪个目录。实测可以排除所有 CLI 侧的来源：

| 来源 | 实测结果 | 结论 |
| --- | --- | --- |
| `thread list --json` | 字段为 `id`、`name`、`generated_title`、`summary`、`keywords`、`created_at_utc`、`archived`、`status`、`index`、`join_id`、`intents` | 无 `cwd`、无仓库名 |
| `thread show --format full/summarize/messages` | `buffer`、`messages`、`comments`、`summary`、`transcript_sections`、`participant_names` | 无 `cwd` |
| `thread search --edited-path <glob>` | 路径是**仓库根相对**且不含仓库标识：`docs/*` 同时命中两个不同项目的线程，`memctl/*`、`internal/*` 之类带仓库前缀的写法全为 0 | 能判断「改过哪些文件」，不能判断「属于哪个项目」 |
| `worktree list --thread <id>` | 只给 `repository`（仓库名）、`worktree_id` 与一个 UUID 形式的 `reference`；帮助文本明确该模式需要登录与 Cloud 访问 | 引入联网依赖，且仓库名在同名仓库上会撞车 |
| `thread tree` | 只有 `root_id`、`root_name`、`current_id`、`threads` | 无路径 |
| Delta 的 checkout 目录名 | 线程 `ksQQUZZp…` 的 checkout 是 `.delta/worktrees/vt74q90bpvrv/memctl` | 短 ID 与线程 ID 之间没有可推导的关系 |

因此方案只采集当前线程：`DELTA_CURRENT_THREAD_ID` 给出线程 ID，进程 cwd 给出项目，两者都在采集时现成可用，不需要任何映射表，也不碰 Delta 的私有目录。

## 4. 采集器契约

### 4.1 入口与触发

```go
// internal/corpus/delta.go
type DeltaRunner func(ctx context.Context, threadID string) (io.ReadCloser, error)

type DeltaRequest struct {
    ThreadID string
    Cwd      string // 已经过 ProjectRoot
    Device   string
}

func CollectDelta(ctx context.Context, run DeltaRunner, cache *Cache, req DeltaRequest) (Digest, bool)
```

- `CollectDelta` 与 `RealTime` 并列，是同一层的第二个入口：`RealTime` 从文件读，`CollectDelta` 从子进程流读。两者最后都调用 `Collector.Collect` 并 `cache.Save`。
- 调用点只有 `dream.refreshCurrentSession`：`req.Agent == "delta"` 时改走 `CollectDelta`，`LocateSession` 对 `delta` 继续返回空串（没有文件）。
- `parsers["delta"]` 注册 `parseDelta`，因此 `Corpus.Supports("delta")` 为真，`sync` / `end` 即使收到 `--agent delta` 也不会误读文件（`TranscriptPath` 为空即返回）。
- `Backfill` 不枚举 `delta`：它没有会话目录，`discoverCandidates` 不新增分支。

### 4.2 解析规则

`parseDelta(c Collector, r io.Reader) (parseInput, bool)` 逐行读 watch 流：

| 输入 | 动作 |
| --- | --- |
| `type: "ready"` | 取 `thread_id` 作为 `parseInput.sessionID`；缺失时用调用方给的 `fallbackSID` |
| `type: "item"` + `kind: "user_message"` | `c.addUser(&pending, &out, content.text)`，与其它宿主共用「连续用户消息合并同一回合」规则 |
| `type: "item"` + `kind: "agent_message"` | 按 §4.3 选一个正文赋给 `pending.Assistant` |
| `type: "item"` + `kind: "tool_call"` | 追加工具签名（§4.4） |
| `type: "item"` + `kind: "agent_thought"` | 丢弃 |
| `type: "item"` + `kind: "external_edits"` / `compaction` | 丢弃 |
| `type: "live"` | 停止读取 |
| 其它行 / 非法 JSON | 跳过（与 `decodeLine` 一致） |

`parseDelta` 不写 `parseInput.cwd`：Delta 不给工作目录，项目键由 `Collector.Cwd`（`ProjectRoot` 的结果）兜底。

### 4.3 回合正文的选择规则

一个回合可能有多条 `agent_message`：它们是 Delta 自己切分的若干段（进度说明 + 最终答复）。统计取自 7 个固定线程（不含调研所在的当前线程，后者边采边增长）：30 个回合中有 28 个带正文，其中 16 个的全部段落拼起来超过 `MaxTurnTextBytes = 1024`（占 57%），直接拼接会让截断点落在进度说明里。

规则：**同回合只保留最长的一条 `agent_message` 正文，等长取最晚的一条**，再交给既有的 `truncateText` 截断。依据是同样 28 个回合中有 27 个的最长段落就是最后一条；最长段落在体积上也天然是最有信息量的那条。

被排除的两种写法：

- **按到达顺序拼接**：16/28 超限，截断点落在进度说明里，最终答复丢失。
- **只取最后一条**：有 1 个回合（多段回合中）最后一段不足 40 字符，是「我先核对一下」这类过渡句，前面的实际答复更长，只取最后一条会留下空壳回合。

### 4.4 工具签名

`tool_call` 转为既有的 `Turn.Tools` 形态，只保留可复核的定位信息：

```text
<name>: <rawInput 的 command | path | pattern | query | url> (<status>)
```

- 依次尝试 `rawInput` 的 `command`、`path`、`pattern`、`query`、`url`，都没有时退到 `title`。
- `status` 实测三个取值：`completed`（1254）、`failed`（11）、`in_progress`（1）。Delta 不提供退出码，失败调用只能靠 `status` 区分。
- `content` 与 `rawOutput` 一律丢弃：单条 `tool_call` 的 `rawOutput` 实测最长 49281 字节。
- `MaxToolsPerTurn = 5` 与 `MaxToolTextBytes = 160` 沿用；`boundTools` 已经对工具签名套同一套脱敏，不需要新规则。

### 4.5 限额、截断与进程处置

| 项 | 取值 | 依据 |
| --- | --- | --- |
| 读取字节上限 | `MaxBackfillFileBytes = 8 MiB`（复用） | 线程 5 在调研期间从 6.2 MB 涨到 7.0 MB，8 MiB 余量已不足 15%；命中即停读并置 `truncated: true` |
| 采集墙钟预算 | `DeltaCollectBudget = 4 s`（新增） | 实测到 `live` 为 1.65–1.93 秒，取 2 倍余量；超时按失败处理，不写缓存 |
| 子进程清理 | 读满字节上限、读到 `live`、超时或调用方取消，都必须终止子进程并回收 | `live` 之后进程常驻（实测再等 5 秒仍在运行） |
| 回合与文本限额 | `MaxTurnsPerSession = 20`、`MaxTurnTextBytes = 1024`、`MaxSessionDigestBytes = 16 KiB` | 沿用 [12 §2.2.3](./12-session-corpus-dreaming.md#3-sessiondigest-数据结构与限额) |

超时与超限都不返回错误、不改退出码：`dream --prepare` 只是拿不到这一份摘要，其余流程照常。

### 4.6 失败处置

| 场景 | 处置 |
| --- | --- |
| `DELTA_CURRENT_THREAD_ID` 缺失 | 无会话 ID，`refreshCurrentSession` 直接返回，不发子进程 |
| `delta` 可执行文件缺失，或版本低于 `0.23.0` | 静默跳过；版本门槛沿用 [13 §5](./13-delta-integration.md#5-适配器契约) 的 `HostMinVersion` |
| `watch` 非零退出或输出非 JSONL | 静默跳过 |
| 达到字节上限、超时、ctx 取消 | 终止子进程，不写缓存（半份摘要会被下一轮当作已入池而永久跳过，规则同 [12 §2.2.2](./12-session-corpus-dreaming.md#2-存量补扫的选择顺序与预算)） |
| 摘要的 `cwdKey` 与当前项目不一致 | 不写缓存（`CollectDelta` 自己算，不依赖日志复核） |
| `AGENT_MEMORY_CORPUS=0` | 与其它宿主一致，整体关闭 |

## 5. 与现有实现的衔接

| 项 | 复用 | 新增 | 改动 |
| --- | --- | --- | --- |
| 清洗与脱敏 | `Clean`、`Redact`、`Suspicious`、`finalizeTurn` | — | 无 |
| 回合与限额 | `addUser`、`boundTurns`、`truncateText`、`boundTools` | — | 无 |
| 缓存与消费 | `Cache.Save/Load/List/Prune`、`WroteMemoryInSess` 回写 | — | 无 |
| 会话身份 | `hookio.ResolveSID` 的 `DELTA_CURRENT_THREAD_ID` 回退 | — | 无 |
| 项目键 | `CwdKey`、`CwdBase` | `ProjectRoot` | `PrepareLocal`、`collectCorpus`、`refreshCurrentSession` 三处调用点 |
| 读取 | `parseFunc` / `Collector.Collect` | `parseDelta`、`DeltaRunner`、`CollectDelta`、`DeltaCollectBudget` | `parsers` 注册表、`refreshCurrentSession` 分支 |
| 适配器 | `delta` 探测条目 | — | 可选：`step:` 中补一条「`dream --prepare` 会采集当前线程」的说明 |

源码落点：`internal/corpus/delta.go`（解析器与 runner）、`internal/corpus/delta_test.go`（解析与边界）、`internal/corpus/corpus.go`（`ProjectRoot` 与常量）、`internal/corpus/corpus_test.go`（归一化）、`internal/dream/local.go`（调用点）、`tests/integration/`（`X10`–`X14` 的端到端项）。

## 6. 对 14 的三处更正

| [14](./14-delta-cwd-feasibility.md) 的判断 | 本机实测 | 影响 |
| --- | --- | --- |
| 「单次最多 20 条，须分页；无总数字段，靠报错推进」（§4、§4.1） | `thread watch` 一次读全，无分页 | 分页器与试错读总数两项实现取消 |
| 「assistant `buffer` 混入 `Thought N times…` 与 `Thinking` 段，需在解析器里剥离」（§4、§4.2） | `agent_message` 与 `agent_thought` 是分属两类 item 的纯正文 | Thinking 剥离规则取消 |
| 「当前线程读不到，须 `env -u DELTA_CURRENT_THREAD_ID`」（§6） | 设置该变量时 watch 正常返回当前线程 | 风险项删除，采集不再需要绕行环境变量 |

[14](./14-delta-cwd-feasibility.md) 的 `ProjectRoot` 结论（§3、§5 第一步）经本轮 4 个 checkout 复测后保持不变。

## 7. 风险与未决问题

| 风险 | 说明 | 处置 |
| --- | --- | --- |
| watch 是未在 [13](./13-delta-integration.md) 记录过的命令 | 它是 Delta CLI 的「Agent integration」分组命令，契约版本 `min=8,max=8`，本轮在 0.23.0 上实测 | 版本门槛保持在 `0.23.0`；升级后先跑 `X13` 再放宽 |
| 固定 1.65–1.93 秒开销 | 每次 `dream --prepare` 都付一次；不影响 `sync` / `write` | 只挂 `prepare`；不进 `Check`、`doctor` 等只读路径 |
| watch 需要登录态 | 帮助文本中 `--thread` 的 worktree 子命令明确要求登录；watch 本身在本机登录态下验证，未在未登录环境复测 | 失败静默跳过；未登录时只是没有 Delta 摘要 |
| 8 MiB 的读取上限面临饱和 | 单个长线程在调研期间从 6.2 MB 涨到 7.0 MB，且无上限 | 命中上限置 `truncated: true`，不当作完整会话；上限值与「持续增长的长线程」的取舍列入实施前的待定项 |
| `decoder` 顺序敏感 | 同一回合时间戳相同，只能按流顺序 | 契约写明按流顺序，不排序 |
| 未验证平台 | 只有 darwin/arm64 与 Delta 0.23.0 | 与 [13 §7](./13-delta-integration.md#7-未验证与后续) 同一范围 |
| 未验证线程形态 | 8 个线程都是本机单作者线程；未测共享线程、子智能体线程、归档线程、Cloud Runner 线程 | 落地前补测归档与共享线程各一次 |
| monorepo 与子目录 | `ProjectRoot` 对 Delta checkout 收敛到仓库根，子包身份丢失 | 与现有宿主一致，按 [14 §6](./14-delta-cwd-feasibility.md#6-风险与未决问题) 的口径不扩展 |

## 8. 验收

| ID | 场景 | 预期 |
| --- | --- | --- |
| `X10` | 在同一项目的原仓库与 Delta 线程 checkout 内分别执行 `dream --prepare` | 两者算出同一个 `cwdKey`；与既有 digest 一致；不新增项目键 |
| `X11` | `ProjectRoot` 输入：`/.delta/worktrees/<id>/<repo>` 与 `/.delta/worktrees/<id>/<repo>/<sub>`、`/.delta/clones/<id>/<repo>.git` 的兄弟路径、普通仓库、普通仓库子目录、非 Git 目录、`git` 不可执行 | Delta 两种形态归一到同一项目根（含子目录）；普通仓库与子目录原样返回；非 Git 目录与无 `git` 时原样返回，不退化为空 |
| `X12` | 在非项目目录（如 `/tmp`）执行 `dream --prepare` 与 `sync` | 不写 digest，不落到其它项目，退出码不变 |
| `X13` | 采集一个 40+ 消息、含 `agent_thought`、含超长 `tool_call`、含 20+ 段 `agent_message` 的线程，且该线程正是当前线程 | 分页无关地取全；正文不含 `Thinking`；每回合保留的是最长段落；单回合 ≤ 1024 字节；`truncated` 与实际截断一致；子进程在读到 `live` 后退出；`dream --prepare` 总耗时包含约 2 秒采集开销 |
| `X14` | `delta` 缺失、版本低于 `0.23.0`、`DELTA_CURRENT_THREAD_ID` 缺失、watch 非零退出、watch 超时、字节上限命中、`AGENT_MEMORY_CORPUS=0` | 七种情况都静默跳过且 `dream --prepare` 退出码为 0；无残留子进程；已完成的其它摘要不受影响 |

`X13` 的样本构造：直接复用本机线程 5（7 条用户消息、千余条 item、采样时已超 6 MB，含数百条 `agent_thought` 与 `tool_call`），并补一个合成 fixture 覆盖「单回合 20+ 段 `agent_message`」与超长 `tool_call`。

## 9. 实施顺序

两步，与 [14 §5](./14-delta-cwd-feasibility.md#5-推荐方案) 一致。第二步的工作量比 [14](./14-delta-cwd-feasibility.md) 估算的小：分页器与 Thinking 剥离两项已不需要，剩下的是一个解析器与一个子进程封装。

**第一步：`ProjectRoot`（`X10`–`X12`）**

- 新增纯函数与三处调用点，`X11` 全部分支可用单元测试覆盖，不需要真实 Delta。
- 对现有宿主是恒等变换，可独立发布。

**第二步：Delta 采集器（`X13`–`X14`）**

- 新增 `parseDelta`、`DeltaRunner`、`CollectDelta` 与预算常量，接入 `refreshCurrentSession`。
- 依赖第一步的项目键：没有归一化，同一仓库会按线程数碎片化。
- 需要真机验证，`X13` 与 `X14` 的「无残留子进程」必须在本机 Delta 终端里实测，不能只靠单元测试。

两步都不改变 CLI 参数、数据格式与既有宿主行为；版本号按向后兼容的能力增量走次版本。

## 10. 结论摘要

- 读取路径从 `thread show` 分页换成 `thread watch`，[14](./14-delta-cwd-feasibility.md) 里的分页与 Thinking 两项成本消失，当前线程也无需绕行环境变量。
- 项目归属仍是 [14](./14-delta-cwd-feasibility.md) 的 `ProjectRoot`；本轮补测确认它在 4 个 checkout 上稳定，且对非 Delta 宿主是恒等变换。
- 线程归属无法从 CLI 得到，因此只采当前线程；这是有意的边界，不是待补的缺口。
- 采集只在 `dream --prepare` 发生，单次固定约 2 秒开销，不进实时路径。
- 新增实现集中在一个解析器与一个子进程封装，其余全部复用现有蒸馏器。
