# memctl-dsh

DSH 宿主插件：会话开始时读取 memctl 记忆并注入上下文，轮次结束后同步，agent 释放时收尾。插件只做生命周期桥接；记忆存储、锁、Git 提交与远端同步由 `memctl` 二进制负责。设计与验收见 [docs/07-dsh-plugin.md](https://github.com/gray0128/memctl/blob/main/docs/07-dsh-plugin.md)。

## 安装

通过 DSH 插件管理器安装 npm 包：

```sh
dsh plugin --profile <profile> add memctl-dsh@0.5.3
```

或使用离线 tarball / 本地目录：

```sh
memctl dsh plugin-path   # 输出绝对 .tgz 路径
memctl dsh plugin-dir    # 输出绝对目录路径
```

安装后由 DSH 加载 `cordis.patch.yml`。卸载：

```sh
dsh plugin --profile <profile> remove memctl-dsh
```

## 行为

- 会话创建时调用 `memctl start --agent dsh --sid <id> --json`，把正文注册为 scoped prompt context。
- 每轮 `turn/end` 后触发有界后台 `memctl sync`。
- agent 释放或插件卸载时尽力执行 `memctl end`。
- 子代理不重复注入或同步。

## 限制

- 只做桥接：不实现记忆写入、检索、向量库或 Dreaming。
- 网络同步、锁和冲突处理由 memctl 负责；失败不阻塞 DSH 会话。
- 未验证的 DSH 版本不会被 `memctl install --status` 报告为 `configured`。

## 许可证

MIT。
