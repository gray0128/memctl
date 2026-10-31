# 错误与恢复

## 退出码

| 退出码 | 含义 | 动作 |
| --- | --- | --- |
| `0` | 成功；Hook 模式下失败只写 stderr | 读取输出确认结果 |
| `1` | 稳定错误，stderr 含 `CODE: message` | 按错误码处置 |
| `2` | 用法错误 | 修正命令，不重试 |

带 `--stdin` 的 Hook 命令为避免阻塞宿主，失败也返回 `0`；以 stderr 和缓存状态判断结果。

## 错误码

| 错误码 | 含义 | 处置 |
| --- | --- | --- |
| `NO_REPO` | 仓库不存在或未初始化 | 请用户运行 `memctl init`，不自行创建仓库 |
| `NOT_FOUND` | 目标文件或路径不存在 | 确认仓库相对路径；新建文件要求父目录已存在 |
| `INVALID_PATH` | 绝对路径、越界路径或 Git 内部路径 | 改为仓库相对路径，不重试 |
| `LIMIT_EXCEEDED` | 超过单文件 2 MiB 或其他请求限额 | 拆分文件或缩短内容 |
| `REVISION_MISMATCH` | 写入基线已过期 | 重新读取、合并最新内容，重试一次 |
| `BUSY` | 仓库锁被占用 | 等待数秒重试一次；仍忙则报告，不删除锁 |
| `EXTERNAL_CHANGES` | 工作树存在未受管的改动 | 停止写入；请用户确认串行维护后用 `sync --capture`，或自行处理 |
| `OPERATION_ACTIVE` | 已有冲突或恢复操作在进行 | 先执行 `resolve --json` 或 `recover --status` |
| `CONFLICT_STALE` | 冲突快照已过期 | 重新执行 `resolve --json`，使用新的 `conflictId` |
| `RECOVERY_REQUIRED` | 存在未完成事务 | 执行 `recover --status`，必要时 `--rollback` |
| `GIT_FAILED` | Git 命令失败 | 报告原始原因，不盲目重试 |
| `IO_FAILED` | 文件或锁访问失败 | 检查权限与沙箱；沙箱内改用 MCP 通道 |
| `NETWORK_UNAVAILABLE` | 远端不可达 | 本地提交保留，稍后重试 |
| `AUTH_FAILED` | 凭据或认证失败 | 请用户修复凭据，不重试 |
| `NO_UPSTREAM` | 未配置上游 | 请用户配置远端与上游 |
| `NON_FAST_FORWARD` | 远端存在新提交 | 先执行 `sync --wait`；出现冲突则进入解决流程 |
| `TIMEOUT` | 操作超时 | 报告并确认没有遗留进程后再试 |
| `UNSUPPORTED` | 当前平台或模式不支持 | 报告限制，不绕过保护 |

## 冲突解决

`memctl resolve --json` 返回当前阶段的 `conflictId`、`baseRevision`、`phase`，以及 `files` 中每个冲突文件的 `base`、`onto`、`replayed` 内容。

准备解决 JSON：

```json
{
  "baseRevision": "<resolve --json 返回的 baseRevision>",
  "resolvedFiles": [
    { "path": "MEMORY.md", "action": "put", "rawContent": "<完整新文本>" },
    { "path": "notes/old.md", "action": "delete" }
  ]
}
```

规则：

- `baseRevision` 必须非空，并取自当前阶段的 `resolve --json`；旧阶段的值不能复用。
- `resolvedFiles` 覆盖当前阶段全部冲突路径，不包含阶段外路径，每个路径只出现一次。
- `action` 为 `put` 或 `delete`；`put` 必须带完整 `rawContent`。
- apply 可能进入下一冲突阶段。重新查看、重新生成 JSON，并使用新的 `conflictId`。
- `--abort` 恢复原始 HEAD。不支持的冲突类型按输出提示交人工处理。

## 同步状态

- `remoteState`：`local_only`、`pending`、`running`、`synced`、`conflict`、`failed`。
- `localState`：`clean`、`external_changes`、`operation_active`、`recovery_required`。
- `pending` 或 `failed` 表示本地提交仍在，稍后重试即可；`conflict` 先解决再写入。
- `aheadCount`、`behindCount` 为 `null` 表示无上游或未知，不按 `0` 处理。

## 中断恢复

- 崩溃或强杀后先执行 `memctl recover --status`，不要直接写入或再次同步。
- `--rollback` 只回滚本工具记录的事务；文件在中断后被修改时拒绝覆盖并给出路径，交用户处理。
- 不手工删除 `memctl.lock`，不执行 `git reset --hard`。

## MCP 通道

- `memory_entries` 按关键词、文件、来源与日期检索条目，返回 `revision`、`items`、`total`；它只读，不建索引、不做语义检索。用它定位事实所在文件，再读取该文件。
- `memory_read` 返回 `filePath`、`fileRevision`、`rawContent`。
- `memory_write` 参数为 `path`、`base`、`content` 与可选的 `sid`；错误通过 `isError` 与 `CODE: message` 返回，处置规则与 CLI 相同。
- `memory_write` 不创建缺失的父目录；写入新路径前确认目录已存在。
- 通道是否可用、某宿主的注册命令：`memctl install --status` 末尾的 `mcp:` 块，或本机 Web UI 的「客户端接入」视图。
