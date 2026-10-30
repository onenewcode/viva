# Linux/Wayland 当前实现与架构复核

- 修订日期：2026-10-30
- 审查基准：`6bc62a4` + Task 3 当前 diff
- 适用分支：`docs/linux-render-refresh-flicker`
- 目标架构：[01-architecture-overview.md](01-architecture-overview.md)
- 待实施任务：[task.md](task.md)
- 范围：Wayland 后端选择、事件循环、surface/configure/呈现、输入调度、缩放、共享 Vulkan WSI、测试与验收边界
- 类型：只读源码审查；本文只保留当前仍未解决的问题和目标差距

> 证据口径：“源码确认”表示控制流中存在对应路径，不表示已在真实 Linux/compositor/GPU 上复现可见症状。Linux 测试交叉编译和 vet 不等于原生测试、race detector 或真实环境验收。已经完成的修复不再作为 findings 保留。

## 1. 结论

**宏观架构合理，不需要推倒重写；当前主路径实现与目标设计基本一致。Tasks 1–3 的当前 diff 已处理本轮三个具体实现问题，剩余为明确的目标架构和验证差距。**

Task 3 将 registry 保留为 application/globals 级对象，使 discovery 后的 output 增删能够进入现有跟踪路径；真实热插拔仍需后续实机验收。

这些问题不需要改变 Wayland-first、PureGo、共享 Vulkan、eventfd 命令队列或 frame-callback 背压等主干设计。应优先修正状态所有权和公开契约，再继续 prepare/read、完整状态机和桌面服务抽象等结构性迁移。

## 2. 当前 findings

本轮没有保留的实现 findings。F1–F3 对应的实现修改均在 Tasks 1–3 当前 diff 中，验证证据和未执行项目见第 5 节；后续 review 若发现回归，应以当前源码重新建立 finding，而不是恢复历史文本。

## 3. 架构合理性判断

### 3.1 后端与共享边界：合理

Wayland-first、强制后端不静默降级、X11 后备，以及共享场景、输入路由和 Vulkan 核心的边界可以继续保留。PureGo 动态绑定也不需要因当前问题改变。

### 3.2 事件循环：主干合理，继续收敛标准读取流程

eventfd 命令队列、主线程执行、FD 异常 readiness、flush POLLOUT 和最近 deadline 调度是合适的职责划分。后续 prepare/read 迁移应保持这些已有行为，重点补齐 `prepare_read` 与 `read_events/cancel_read` 的配对，而不是重新引入平行 ticker 或另一套 FD 所有权。Task 2 diff 也将 `Frame` 改为只读主线程发布的不可变几何快照，避免后台直接读取窗口字段。

### 3.3 Surface/configure/呈现：事务方向正确，几何所有权仍需集中

scene update 与 present/commit 分离、clean/forced present、frame callback 配对以及 frame-pending 时延迟提交，都是正确主干。当前 Task 1 diff 进一步区分了 toplevel 待闭合、serial 待 ACK 和 hidden/frame-pending 延迟的几何所有权，并阻止 serial 到达前提前呈现。

完整显式状态机仍属于目标架构差距，不应把每一次协议交错都继续转换成新的布尔特判。

### 3.4 Vulkan WSI：恢复边界合理

有界恢复、swapchain 重建后的再次呈现以及资源集中销毁符合目标设计。当前 findings 不要求调整 Vulkan 主干；后续重点是 validation/lavapipe 和真实驱动验收。

### 3.5 缩放与多输出：主路径合理，动态生命周期未闭合

fractional-scale + viewport、整数 preferred scale 和 output scale 的优先级合理。Tasks 1–2 的当前 diff 覆盖几何交错与跨线程快照，Task 3 又将 registry 生命周期延长至 globals teardown，使晚期 output 增删能够驱动 scale 回退；真实热插拔结果仍列入后续验收。

## 4. 尚未完成的目标架构能力

以下是目标差距，不与第 2 节的确定性实现问题混为一谈：

| 能力 | 当前缺口 |
| :--- | :--- |
| 后端探测与错误链 | 自动后备仍丢弃 Wayland 初始化原因，选择阶段的全局 hook/服务启动与目标约束尚未收敛 |
| libwayland prepare/read 标准流程 | 尚未形成完整 prepare/read/cancel 配对 |
| 显式 surface/configure 状态机 | serial、ACK、几何和提交事务仍需进一步集中 |
| 通用 event-source 抽象 | FD 与 callback 的具体实现尚未统一为带 Interest/Dispatch 契约的接口 |
| 桌面服务公共生命周期 | 协议核心部分共享，连接和重连状态机仍有后端分叉 |
| 托盘/后台常驻 | 完整 keep-alive 产品契约尚未实现 |
| 原生 Linux race 验证 | Task 2 并发快照测试已交叉编译，尚未在 Linux/race detector 下执行 |
| 真实环境验收 | headless compositor、Vulkan validation/lavapipe、真实桌面与性能矩阵未完成 |

`01-architecture-overview.md` 已同步当前基线。后续实施任务完成后，应再次更新三份 Wayland 文档的审查基准和状态，避免已解决问题重新成为待办。

## 5. 本地验证记录

审查环境为 macOS，未提供真实 Linux/Wayland/Vulkan 运行环境。以下命令成功：

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go test -c -o /tmp/viva-linux-platform-task3.test ./internal/platform/linux

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go vet ./internal/platform/... ./app
```

这些结果只证明相关 Linux 测试可交叉编译、静态检查通过。Task 3 新增的 registry 延迟销毁、晚期 output 新增/移除、窗口 scale 回退和销毁计数测试均包含在上述测试包中，但没有原生执行，也没有真实热插拔验收。本次没有执行 Linux 测试二进制、race detector、compositor、Vulkan WSI/validation 或真实桌面/性能验收；原生 race 验证已列入 Task 8。

## 6. 建议优先级

1. **P1：完成阶段文档一致性检查**：同步 Tasks 1–3 的实现状态与剩余验证证据。
2. **P2：继续结构性收敛**：后端探测与错误链、prepare/read、统一 event-source、显式 surface/configure 状态机。
3. **P3：补真实验证层次**：原生 Linux race、headless compositor、validation/lavapipe、真实 GNOME/KDE/Sway 和性能矩阵。

最终判断：**保留现有架构主干；Tasks 1–3 的实现候选已覆盖本轮具体源码问题，但不能称为已完成原生 race、真实 compositor、热插拔或 Vulkan/桌面矩阵验收。**
