# 开发状态

本文件是开发任务状态与证据的唯一台账。状态含义及完成条件见 [开发循环规范](./06-development-loop.md)，功能开放顺序见 [验收与交付](./05-validation-and-delivery.md#1-交付顺序)。

## 当前事实

- 已完成阶段 A-1（T003）：建立 `go.mod`、`Makefile`、`cmd/memctl` 与 `internal/cli` 最小命令入口，支持 `version/help`、用法错误返回（退出码 `2`）及未实现子命令显式拒绝（退出码 `1`），通过 `R03` 验收（macOS arm64，Go 1.27.1）。
- 已完成阶段 A-2 的初始化与读取部分（T006）：`init` 仅本地创建或复用记忆仓库，`read` 输出原文或 `{filePath, fileRevision, rawContent}`，读取经根句柄并有界；通过 `G01`、`G05` 的本地初始化场景、`F04`、`P01`、`P02` 的读取场景和 `P03` 的读取限额，`R03` 回归通过。
- 尚未实现记忆格式与链接解析（A-2 余项，T008）、受管写入与恢复（A-3）、远端同步（A-4）、Hook（A-5）、前端工程或 Web UI；05 中其余产品验收场景仍待实现与验证。
- 本轮完成 T006，并因此调整 R03 集成测试：`init` 从「未实现命令」改为「已实现但缺少 Git 时干净失败」。

## 任务

| ID | 状态 | 目标与依赖 |
| --- | --- | --- |
| T001 | 已完成 | 建立项目入口、目录职责、开发循环和本台账；证据见下文 |
| T002 | 待开始 | 补齐凭据契约与 G08 场景；A-4 的前置设计任务，不阻塞本地开发 |
| T003 | 已完成 | 对应 A-1：建立最小 Go 工程、`version/help` 和实际构建测试入口；证据见下文 |
| T004 | 已完成 | 在 AGENTS 中明确文档工作必须使用 `tech-doc-style-chinese` |
| T005 | 已完成 | 补充阶段 A 的实施顺序、依赖、交付物和近期任务验收条件 |
| T006 | 已完成 | 对应 A-2：本地初始化与读取；依赖 T003；证据见下文 |
| T007 | 待开始 | 对应 A-3：受管写入与恢复；依赖 T006 |
| T008 | 待开始 | 对应 A-2：记忆格式与链接解析；依赖 T003，不阻塞 T007 的例程设计 |

任务 ID 不代表执行顺序。T006 已完成；建议依次推进 T008、T007，T002 在 A-4 开始前完成。A-4 之后的具体任务临近实施再登记。

顺序、交付边界和验收映射的权威定义见 [阶段 A 计划](./05-validation-and-delivery.md#阶段-a-的实施顺序)，本表只维护执行状态。A-2 拆为 T006（初始化与读取）和 T008（格式解析），两者范围不重叠。

## T006：本地初始化与安全读取

- 目标与范围：完成 A-2 的初始化与读取部分；新增 `internal/config`、`internal/git`、`internal/vault` 的路径与读取部分、`internal/app` 的 `Init`/`ReadFile`、`internal/cli` 的 `init` 与 `read`，以及单元与集成测试。不实现格式解析（归 T008）、受管写入与仓库锁（归 T007）、远端或凭据行为。
- 契约：[仓库身份与文件边界](./01-architecture-and-modules.md#3-仓库路径与信任边界)、[初始化流程](./03-workflows-and-dataflows.md#1-初始化与远端接入)、[修订标识与限额](./02-domain-models-and-contracts.md#1-修订标识与格式解析)、[CLI 命令](./04-ui-and-cli-specification.md#1-cli-命令)。
- 验收：`G01`、`G05` 的本地初始化场景、`F04`、`P01`、`P02` 的读取场景、`P03` 的读取限额；`R03` 回归。
- 验证日期与环境：2026-10-16，macOS arm64，Go 1.27.1，Git 2.54.0，GNU Make 3.81。
- 命令：`make build` 与 `make test` 全部通过。`tests/integration/a2_init_read_test.go` 编译真实二进制并在临时 `HOME`、临时仓库下运行，子进程环境设置 `GIT_ALLOW_PROTOCOL=file`、`GIT_TERMINAL_PROMPT=0`，且不配置任何 remote；本地命令不访问网络。
- G01：连续两次 `init` 均退出码 `0`，第二次报告 `state: existing` 且 head 不变；`git rev-list --count HEAD` 为 `1`，`MEMORY.md` 字节不变，工作树干净，无 remote，运行状态目录未被创建。已存在的空目录报告 `state: created`；已存在文件、非空普通目录、父目录为 Git 仓库、无关 Git 仓库四类目标均返回 `INVALID_ARGUMENT`，且不修改原文件、不创建 `.git` 或 `MEMORY.md`。
- G05（本地部分）：以 `user.useConfigOnly=true` 模拟无身份环境时，`init` 退出码 `1`，stderr 为 `GIT_FAILED` 并提示配置本仓库身份，全局配置字节不变；随后写入仓库本地身份再执行 `init`，报告 `state: completed` 且提交数仍为 `1`。`pre-commit` 钩子拒绝、`commit.gpgsign=true` 配合失败签名程序两种场景均退出码 `1`，钩子文件与 `commit.gpgsign` 保持不变。
- F04：`read MEMORY.md` 输出与磁盘字节一致；`read --json` 输出 `filePath`、`fileRevision`、`rawContent`，`fileRevision` 为 `sha256:` 加 64 位十六进制、重复读取稳定、文件模式变化时改变。不存在、二进制资产、超过 2 MiB、目录分别返回 `NOT_FOUND`、`UNSUPPORTED`、`LIMIT_EXCEEDED`、`INVALID_PATH`，stdout 为空。单元测试锁定 `sha256:3d3ad089ac30852a08937bc697caebe8b0c7a39f8d15648b2a126ad61ad47f6a`，确认修订采用长度前缀的「Git 模式 + 原始字节」编码。
- P01、P02（读取）：`../outside.md`、`/etc/hosts`、`.git/config`、`.GIT/HEAD`、`NUL`、含反斜杠路径均返回 `INVALID_PATH`；符号链接及其目录组件返回 `UNSUPPORTED`，未输出库外内容；所有失败路径 stdout 为空。
- P03（读取）：等于 2 MiB 的文件可读，超出 1 字节即返回 `LIMIT_EXCEEDED`。stdin、HTTP 请求体与变更文件数上限不在本轮范围。
- 单元测试：`internal/config`、`internal/git`、`internal/vault` 覆盖默认与覆盖路径解析、`repoId` 稳定性、`Canonical` 对已存在前缀的符号链接解析、Git 重定向变量剔除与凭据 URL 脱敏、路径边界表、修订编码与有界读取。
- R03 回归：`init` 已实现，因此 R03 集成测试改用仍未实现的 `write` 保留「无占位成功结果」检查，并新增「`PATH=""` 下 `init` 退出码 `1`、stderr 含 `GIT_FAILED`、stdout 为空且不创建仓库目录」；`version/help` 的隔离要求保持不变。
- 未运行：格式与链接解析场景 `F01`、`F02`（归 T008）；写入、锁与事务场景（归 T007）；远端、Hook、UI 与平台生命周期测试；未在 Windows 或 Linux 上运行。
- 遗留限制：`fileRevision` 的文件模式取自文件系统权限位，未与 Git 索引模式比对；硬链接目标仅禁止写入，读取仍允许，多硬链接写入检查在 A-3 落地时补测。
- 下一步：建议 T008（记忆格式与链接解析）；本轮不自动推进。

## T003：工程与命令入口

- 目标与范围：完成 [阶段 A-1](./05-validation-and-delivery.md#阶段-a-的实施顺序)；新增 `go.mod`、`Makefile`、`cmd/memctl/main.go`、`internal/cli/cli.go`、`internal/cli/cli_test.go`、`tests/integration/r03_cli_test.go`，并同步根 `README.md` 与本台账。不创建空源码目录、前端目录或占位成功子命令。
- 契约：[架构与目录](./01-architecture-and-modules.md#5-目录结构与依赖)、[CLI 规范](./04-ui-and-cli-specification.md#1-cli-命令)、[阶段 A 计划](./05-validation-and-delivery.md#阶段-a-的实施顺序)。
- Go 版本与模块依据：选用当前环境受支持的稳定版 `Go 1.27.1`（`go.mod` 声明 `go 1.27.1`），满足 01 中 Go 1.22+ `ServeMux` 路由语法及后续 `os.Root` 根句柄安全文件访问要求；模块路径按仓库 `origin` 定为 `github.com/gray0128/memctl`，A-1 阶段零外部第三方依赖。
- 验收：`R03`（macOS arm64）、`DOC01`、`DOC02`。
- 验证日期与环境：2026-10-16，macOS arm64，Go 1.27.1，Git 2.54.0，GNU Make 3.81。
- R03：执行 `make build` 与 `make test` 通过。`internal/cli` 单元测试覆盖 `version`/`--version`/`-v`、`help`/`--help`/`-h`、缺少子命令、未知子命令、未知参数、`version/help` 多余参数，以及 16 个后续规划子命令的显式非零拒绝（退出码 `1`，`UNSUPPORTED`，无空实现返回 `0`）。`tests/integration/r03_cli_test.go` 在临时目录编译真实二进制，并在 `PATH=""`、指向不存在路径的 `HOME`/`AGENT_MEMORY_DIR`/`AGENT_MEMORY_STATE` 隔离环境下验证：`version` 与 `help` 退出码为 `0` 且不创建或访问任何仓库/状态目录、不依赖 Git 或 Node.js；未知命令退出码为 `2`；未实现命令退出码为 `1`。
- DOC01 与 DOC02：核对根 `README.md`、`AGENTS.md` 与 `docs/` 全部本地链接及锚点有效；源码目录仅包含已落地文件的 `cmd/memctl`、`internal/cli` 和 `tests/integration`，无空目录或 `.gitkeep`。行尾空白扫描无匹配。注：当前环境未安装 `tech-doc-style-chinese`，本次对 `README.md` 与 `docs/STATUS.md` 的更新沿用仓库既有文档风格基线并待确认。
- 未运行：A-2 及之后的仓库读写、Git 同步、前端构建与跨平台测试（不属于 A-1 范围）。
- 下一步：建议确认文档替代方式后，从 T006（对应 A-2：本地初始化与读取）或 T002（凭据契约）继续；本轮不自动推进。

## T005：阶段 A 开发计划

- 范围：修改 05 的阶段计划和必要验收场景，细化本台账的近期任务；不另建计划文件，不展开 B–D 的具体任务。
- 验收：阶段 A 的每一步均有依赖、交付物和验收映射；近期任务可判断完成；凭据决策不阻塞纯本地开发；原有验收 ID 保留，文档链接有效。
- 验证：2026-10-16，macOS，使用 Ruby 内联只读检查遍历根入口及 docs 的链接和锚点，10 份文档的 57 处本地链接有效；验收矩阵共 50 个唯一 ID，原有 45 个均保留，新增 F04、G08、H06、R03、R04。检查 7 个任务引用和 6 个阶段步骤的依赖，无循环或前置顺序错误。
- 人工核对：各阶段与近期任务都有交付边界和验收映射；凭据仅阻塞远端同步，B–D 未提前拆分，复合用例按子场景验收。两份改动文件行尾空白扫描无匹配。
- 未运行：本轮只更新计划，未执行产品测试、构建或平台认证；新增场景仍待实现与验收。
- 下一步：建议获实施授权后启动 T003；本轮不自动开始编码。

## T004：文档 Skill 使用要求

- 范围：只修改 `AGENTS.md` 的文档规范入口和本台账，不复制 Skill 正文。
- 验收：指明适用文档操作、准确的 Skill 名称、编辑前读取与应用要求，以及 Skill 不可用时的处理方式；保留原有协作规则。
- 验证：2026-10-16，macOS；读取 AGENTS 核对上述验收项均满足，原有 5 条规则保留；`rg -n '[ \t]+$' AGENTS.md docs/STATUS.md` 无匹配。本轮未改变链接或产品契约，不运行产品测试。

## T001：项目入口与开发循环

- 目标：新执行者能找到权威设计、判断哪些目录已存在、按一套最小流程开发，并识别尚未完成的工作。
- 范围：根 `README.md`、`AGENTS.md`、`.gitignore`；01 的目录规划；docs 索引；06 循环规范；本台账。参考脚本不变，不生成空源码骨架。
- 契约：[目录结构](./01-architecture-and-modules.md#5-目录结构与依赖)、[开发循环](./06-development-loop.md)。
- 验收：DOC01，本地链接与锚点均有效；DOC02，目录与实际阶段一致；DOC03，流程具有开始条件、失败分支、完成条件和接续入口；DOC04，没有把规划、文档检查或参考脚本标为产品验收通过。
- 验证日期与环境：2026-10-16，macOS arm64，Go 1.27.1，Git 2.54.0。
- DOC01：解析根 README、AGENTS 与 docs 共 10 份 Markdown 的本地链接，再对照目标文件和标题锚点，49 处均有效；12 个代码块闭合，JSON 和 Go 示例语法通过检查。临时检查器在工作区外运行，本条记录保留可复核步骤，不依赖临时文件长期存在。
- DOC02：使用 `rg --files --hidden -g '!.git' -g '!.DS_Store'` 对照目录说明，已存在入口与规划源码区分一致；没有创建空源码目录。用 `git check-ignore --no-index` 抽查，8 个构建产物或本地配置路径被忽略，7 个文档、源码或示例路径未被忽略，后者退出 1 表示无匹配。
- DOC03：逐项核查 06 的步骤和状态转换，具备开始条件、验证失败回到实现、阻塞解除、完成证据和中断接续规则；根 README 与 AGENTS 都能直接找到该入口。
- DOC04：逐项核查项目阶段与测试声明，方案、候选平台和参考脚本均未标为实现完成。另以标准库对 22 条既有 API 模式进行注册和匹配探测通过，仅证明文档路由有效，不构成产品 API 验收。
- 最终差异：已读取本次新增及修改的文件，行尾空白扫描无匹配；只改入口、规范、目录规划和忽略配置，参考目录保持不变。
- 未运行：二进制、前端、平台及宿主测试，本轮纯文档范围不适用。
- 下一步：建议从 T002 或 T003 选择下一轮；本轮不自动推进。

## 近期实施任务

### T008：记忆格式与链接解析

- 目标与依赖：完成 A-2 的解析部分，前置为 T003；与 T007 范围不重叠。
- 交付物：`internal/vault` 的行级无损解析（单行 bullet、行尾元数据、多个 `[source: ...]`、未知键、根相对 `[[path]]`、代码围栏与行内代码排除）、条目与链接标识、同名歧义报告，以及覆盖 F01、F02 的单元测试。
- 契约：[修订标识与格式解析](./02-domain-models-and-contracts.md#1-修订标识与格式解析)、[领域对象](./02-domain-models-and-contracts.md#2-领域对象)。
- 完成检查：`F01`、`F02` 通过；未编辑内容逐字节保留；解析不写入仓库；回归 T006 的读取测试。

### T007：受管写入与恢复

- 目标与依赖：完成 A-3，前置为 T006 完成；形成可验证的本地读写闭环。
- 交付物：受管写入、仓库锁与版本校验、事务恢复记录、显式本地 capture、recover 和中断测试；多文件事务从应用服务验证，不提前实现 Web UI。
- 契约：[写入模型](./01-architecture-and-modules.md#2-写入模型)、[事务与恢复](./03-workflows-and-dataflows.md#2-受管写入事务)。
- 完成检查：A-3 对应场景通过并回归 A-1、A-2；在实际测试平台验证子进程生命周期。此结果不代表 W02 的 UI 并发或任何未测平台已通过。

### T002：凭据契约

- 目标与依赖：为 A-4 提供可实施契约，可在本地开发之前或期间单独完成；此任务只完成设计与场景定义，不把 G08 标为测试通过。
- 交付物：01 的凭据与信任边界、03 的环境继承和失败处理流程、05 中 G08 的可复现子场景，必要时同步 04 的用户操作说明。
- 完成检查：SSH/HTTPS 路径分别明确，终端与后台环境差异、交互提示、总超时和错误分类均有确定处理规则及预期结果；影响结果的未决选项获得确认。

当前设计复用系统 Git/SSH 环境，但还没有可实施的后台凭据继承清单和提示抑制协议。该任务需明确 `SSH_AUTH_SOCK`、HTTPS helper、ASKPASS 等环境如何继承或限制，如何处理交互式 helper，以及鉴权失败与超时如何区分。

结果写回 [架构](./01-architecture-and-modules.md)、[同步流程](./03-workflows-and-dataflows.md) 和 [验收矩阵](./05-validation-and-delivery.md)，不在台账定义第二套凭据规则。覆盖「终端认证成功但后台环境不同」的场景；未经验证，不宣称后台一定复用成功。
