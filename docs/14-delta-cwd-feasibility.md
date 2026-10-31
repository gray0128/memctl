# 14. Delta 会话蒸馏的 cwd 方案可行性

> 状态：调研完成（2026-10-31，Delta 0.23.0 / darwin arm64），未实现。本文只回答一个问题：能否用「当前工作目录」给 Delta 会话确定项目归属，从而接入 [12](./12-session-corpus-dreaming.md) 的本地会话蒸馏。结论见 §1，取舍与剩余风险见 §5、§6。宿主接入面见 [13. Delta 宿主接入](./13-delta-integration.md)。本文 §4、§4.1、§4.2 与 §6 中关于分页、Thinking 混入与当前线程可读性的三项判断已被后续实测推翻，以 [15. Delta 会话蒸馏适配](./15-delta-corpus-adaptation.md#6-对-14-的三处更正) 为准；§3、§5 第一步的 `ProjectRoot` 结论保持不变。

## 1. 结论

**结论：cwd 方案可行，但前提是把 cwd 归一化成「项目根」，不能直接拿进程的工作目录当项目键。** 归一化后，Delta 线程里的会话能与现有记忆库中的项目键对上，不产生新的碎片。

三条支撑事实：

1. `corpus.CwdKey` 是 `sha256(cwd)` 的完整摘要（[corpus.go](../internal/corpus/corpus.go)），所以**多一个字节就多一个项目**。朴素的「用 cwd 当键」在 Delta 上必然碎片化：Delta 为每个线程创建独立的 managed checkout，同一仓库在不同线程下路径不同。
2. 本机实测：同一仓库 `memctl` 的原仓库与两个线程 checkout 得到 3 个不同 key；而把三条路径都归一化到项目根后，三者收敛为同一个 key，且与现有 57 条 digest 记录的 `cwdKey` **逐字节相同**。
3. 归一化有两条互相独立的规则，在 3 个真实样本上结论一致：`git rev-parse --git-common-dir` 截断到 `/.delta/clones/` 之前，以及 `git rev-parse --show-toplevel` 截断到 `/.delta/worktrees/` 之前。

**但可行性不等于可以直接落地。** cwd 只解决「项目归属」这一个问题；Delta 会话蒸馏还缺三个能力：没有会话日志文件、消息要经 CLI 分页取、assistant 文本混入了思考摘要。这三项决定实现成本与收益（§4、§5）。

判据：cwd 方案的成立条件是「Agent 在项目 checkout 内执行 `memctl`」，不成立时该会话不产生摘要，而不是落到错误的项目上。

## 2. 问题重述

[12 §2.2](./12-session-corpus-dreaming.md#22-本地会话采集与确定性蒸馏internalcorpus) 的补扫顺序是「先按项目收窄，再按时间排序」。项目收窄的依据是 `SessionDigest.cwdKey`，由会话日志自己记录的 `cwd` 算出。Delta 的会话记录里没有这个字段，于是补扫只能退化成「全机扫描」，把无关项目的会话塞进当前项目的整理上下文。

cwd 方案的思路是换一个来源：**不看会话日志说什么，看 Agent 干活时站在哪个目录**。`memctl` 由 Agent 在终端里执行，进程的 `cwd` 就是项目 checkout。

## 3. 实测证据

测量环境：Delta 0.23.0，darwin/arm64，2026-10-31。样本为本机 6 个 Delta 线程、202 份现有 digest。

### 3.1 朴素 cwd 会碎片化

| 路径 | `sha256(路径)` 前 26 位 | 是否已存在于 digest |
| --- | --- | --- |
| `/Users/libo/Documents/github/memctl` | `sha256:bff0d2a77277ad19d3d…` | 是（57 条） |
| `…/.delta/worktrees/vt74q90bpvrv/memctl` | `sha256:6c7adade20409a1d39b…` | 否 |
| `…/.delta/worktrees/eh1c2q2xgcqy/memctl` | `sha256:9a620445bdd8bfeaab0…` | 否 |

同一仓库 3 个 key。若直接采用，每开一个 Delta 线程就给该项目造出一个新项目键，`--prepare` 的「当前项目优先」排序失效，同一项目的会话被切碎。

### 3.2 归一化到项目根后收敛

| 检查 | 结果 |
| --- | --- |
| `--git-common-dir` 规则给出的根 | `/Users/libo/Documents/github/memctl` |
| `--show-toplevel` 规则给出的根 | `/Users/libo/Documents/github/memctl` |
| 两规则一致 | 是（3 个样本全部一致） |
| 归一化后的 key | `sha256:bff0d2a77277ad19d3d…`，与现有 57 条 digest 逐字节相同 |

归一化实现只需要一条命令，两种写法任选：

```text
git -C "$PWD" rev-parse --git-common-dir    # /…/memctl/.delta/clones/<id>/memctl.git
  → 截断到 "/.delta/clones/" 之前即为项目根
git -C "$PWD" rev-parse --show-toplevel     # /…/memctl/.delta/worktrees/<id>/memctl
  → 截断到 "/.delta/worktrees/" 之前即为项目根
```

两条规则都不依赖 Delta 的私有文件或 DeltaDB，只用标准 Git 元数据。

### 3.3 现有蒸馏器能消化 Delta 的消息形状

`delta thread show --format messages` 返回的每条消息带 `number`、`role`、`buffer`、`tool_calls[]`（含 `name`、`input`、`output`、`status`），与 [collect.go](../internal/corpus/collect.go) 需要的「用户文本 + 助手文本 + 工具签名」结构对齐。单线程实测：4 条消息，assistant 侧 24 与 4 个工具调用。

## 4. 与现有实现的差距

cwd 只补上「项目归属」，其余四项仍需新增实现：

| 能力 | 现状 | Delta 上的情况 | 结论 |
| --- | --- | --- | --- |
| 项目归属 | `cwdKey` 来自日志 `cwd` | 日志不存在；改由进程 cwd 归一化 | **cwd 方案可解** |
| 会话日志来源 | `transcript_path` 或 `LocateSession(sid)` 定位文件 | 无文件；只有 DeltaDB 与 CLI | 必须新增 CLI 读取路径 |
| 会话 ID | Hook payload / 环境变量 | 已有 `DELTA_CURRENT_THREAD_ID`（[13 §4](./13-delta-integration.md#4-会话身份)） | 已解决 |
| 消息完整性 | 一次读完整个文件 | 单次最多 20 条，须分页；无总数字段，靠报错推进 | 需分页器 |
| 文本质量 | 宿主日志的最终文本 | assistant `buffer` 混入 `Thought N times…` 与 `Thinking` 段 | 需在解析器里剥离 |

### 4.1 分页成本

消息范围上限 20 条（`--messages 1-25` 直接报 `message range must contain 1 to 20`）。判断总数只能靠试探：请求 `1-20` 时若报 `message range 1-20 is outside this thread (N messages)`，即可读出总数 N。实测本机 6 个线程分别为 2、4、4、14、16、≥20 条。

单次 `thread show` 延迟约 0.2–0.3 秒，`--format full` 读取一个 1.5 MB 的线程也是 0.31 秒。因此一个 40 条消息的线程需要约 2–3 次调用，落在 [12](./12-session-corpus-dreaming.md) 的实时采集预算内，但不能对全机线程无差别调用。

### 4.2 文本质量

实测一条 assistant 消息的 `buffer` 开头是：

```text
Thought 18 times, ran 18 commands, called 2 tools, fetched 4 URLs
Thinking
The user asks whether Delta loads skills from …
```

`buffer` 是渲染后的整体，思考段与正文没有结构化分隔。`corpus.Clean` 现有规则处理代码围栏、系统块与凭据，**不剥离 Thinking**。直接送进蒸馏会让摘要以推理过程为主，而 [12 §2.3](./12-session-corpus-dreaming.md#23-本地当前-agent-交互式整理协议prepare--propose--apply) 要的是结论。`MaxTurnTextBytes = 1024` 会把 60 KB 的 assistant 文本截到 1 KB，若截断点落在思考段，正文可能整段丢失。

## 5. 推荐方案

分两步，先解决归属、再决定是否接内容。

**第一步（建议采纳）：把 cwd 归一化写进契约，作为 Delta 的项目键来源。**

- 新增 `corpus.ProjectRoot(cwd)`：先 `git rev-parse --git-common-dir`，命中 `/.delta/clones/` 则截断；否则落在 `/.delta/worktrees/` 则截断；都不命中时原样返回。实现细节（`--path-format=absolute` 不可省、候选路径需存在）见 [15 §3.1](./15-delta-corpus-adaptation.md#31-corpusprojectrootcwd)。
- `dream --prepare` 的 `Cwd` 与 `sync` 的 cwd 都过这一层，Delta 与其它宿主共用同一条规则。这对现有宿主是恒等变换（它们的日志 `cwd` 本来就是项目根），风险仅限于「Agent 在 Delta 线程里 cd 到子目录」——归一化到仓库根正是期望行为。
- 这条改动**不引入 Delta 专属分支**，也不新增依赖，可独立验证。

**第二步（需单独决策）：新增 Delta 采集器。**

> 本节与下文的读取路径已被 [15 §2](./15-delta-corpus-adaptation.md#2-读取路径) 取代：实测确认 `delta thread watch <id> --json` 一次读完、正文与思考段分属不同事件，因此分页器与 Thinking 剥离规则都不需要，当前线程也不需要绕行环境变量。采集器契约以 [15 §4](./15-delta-corpus-adaptation.md#4-采集器契约) 为准。

- 新增 `internal/corpus/delta.go`：`parseDelta` 消费 CLI 的分页结果，`LocateSession` 对 `delta` 返回空（无文件），改由一个新的 `CollectThread(threadID, cwd)` 入口驱动。
- 需要解决的问题：分页器（最多 20 条/次、试错读总数）、Thinking 剥离规则、`LiveBudget` 与 CLI 延迟的匹配（0.2–0.3 秒/次 × N 页）、以及「哪些线程属于当前项目」的筛选（用 §3.2 的归一化根与线程 checkout 的 `git-common-dir` 比对）。
- 触发时机：只在 Agent 主动执行 `memctl sync`/`dream --prepare` 时采集当前线程，不做全机后台扫描。Delta 没有 `end`，这已是唯一可靠时机。

## 6. 风险与未决问题

| 风险 | 说明 | 处置建议 |
| --- | --- | --- |
| Agent 不在项目目录内执行 | 用户在 Delta 里 `cd /tmp` 后让 Agent 写记忆 | 归一化失败时返回空 cwd，该会话不生成摘要（保持现状的静默跳过），不落到错误项目 |
| monorepo 子目录 | `--show-toplevel` 会收敛到仓库根，丢掉子包 | 与现有宿主行为一致（日志 `cwd` 通常也是仓库根），不在本轮扩展 |
| 非 Git 目录 | `/tmp`、`~/` 执行 `git rev-parse` 返回 128 | 归一化函数原样返回，保持 `CwdKey` 语义不变 |
| 当前线程读不到 | `thread show <当前线程>` 报错，须 `env -u DELTA_CURRENT_THREAD_ID`（后续实测：`thread watch` 无此限制，见 [15 §2](./15-delta-corpus-adaptation.md#2-读取路径)） | 这是 `thread show` 的边界行为，不宜写进契约；第一步不依赖它 |
| 直读 DeltaDB | 官方要求只用 CLI | 不采用，即使 SQLite 里字段更全 |
| 未验证 | 只在 darwin/arm64 与 Delta 0.23.0 实测，样本 6 个线程 | 落地前需在 2–3 个真实项目、含 monorepo 的仓库复测 |

## 7. 建议的验收

| ID | 场景 | 预期 |
| --- | --- | --- |
| `X10` | 在 Delta 线程 checkout 内与在原仓库内分别执行 `dream --prepare` | 两者算出同一个 `cwdKey`；与既有 digest 记录一致；不新增项目键 |
| `X11` | cwd 位于 `/.delta/worktrees/<id>/<repo>`、`/.delta/clones/<id>/<repo>.git` 的兄弟路径、非 Git 目录、monorepo 子目录 | 前两者归一到同一项目根；非 Git 目录原样返回；monorepo 收敛到仓库根 |
| `X12` | Agent 在非项目目录执行 `memctl sync` | 不写入 digest，不落到其他项目，退出码不变 |
| `X13`（第二步） | 采集一个含 40+ 消息、含 `agent_thought` 的 Delta 线程 | 以 [15 §8](./15-delta-corpus-adaptation.md#8-验收) 的 `X13` 为准：不分页取全、正文不含 `Thinking`、保留最长段落、子进程在 `live` 后退出 |

## 8. 结论摘要

- cwd 方向可行，条件是**归一化到项目根**，否则同一仓库会按线程数碎片化。
- 归一化只需要标准 Git 元数据，两条独立规则互相印证，且与现有 57 条 digest 完全对齐。
- cwd 只解决项目归属；会话日志缺失、CLI 分页、Thinking 混入是另外三项成本，属第二步。其中后两项已被 [15](./15-delta-corpus-adaptation.md) 的实测推翻。
- 建议先落第一步（纯函数 + 契约 + `X10`–`X12`），再单独评估第二步是否值得；第二步的具体契约见 [15](./15-delta-corpus-adaptation.md)。
