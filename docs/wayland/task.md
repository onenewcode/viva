# Wayland 未完成任务

- 更新日期：2026-10-30
- 基线：`acf95b2` + Task 8 可用环境矩阵
- 开发契约：[01-architecture-overview.md](01-architecture-overview.md)
- 当前 findings：[02-implementation-architecture-review.md](02-implementation-architecture-review.md)

本清单只保留未完成事项。P1 的 Task 11 与 P3 的 Task 8 已完成并从本清单移除；Task 8 在受限环境中无法执行的项目（GNOME/Sway/IBus、真实 portal/session bus 重启、device loss 注入、运行时 output 热插拔）作为限制记录在 [02-implementation-architecture-review.md](02-implementation-architecture-review.md) §4.5，不声称已验证。

## 执行规则

1. 按 P1 → P2 → P3 执行；同优先级按任务编号推进。
2. 代码任务必须包含实现和行为回归测试，不能用删除旧断言代替验证。
3. 每个任务完成后进行独立只读 review，阻断问题修复并复核；连续两次 PASS 后再提交并推送。
4. 代码任务至少执行 gofmt、`git diff --check`、Linux 交叉 vet、Linux 测试包交叉编译和当前环境可运行的相关测试。
5. 同步更新三份文档的基线、findings 和验证证据；完成项从当前清单删除，不保留修复流水账。
6. 原生单元测试、race、headless compositor、Vulkan validation 和真实桌面结果分别记录，不能互相替代。

## 尚未排期

- 受限环境验收：GNOME、Sway/wlroots、IBus、拖放、AT-SPI、真实 session bus/portal 重启、device loss 注入和运行时 output 热插拔。需要解锁的真实会话或可控测试机，完成前不得以 fake 或 headless 结果替代。
- 桌面服务公共连接/重连抽象：待真实服务重连验收后实施，保留各后端 FD 与显示连接适配。
- 托盘和显式后台常驻：先明确 keep-alive 产品契约，避免普通 watcher 意外延长应用生命周期。
