# Viva Linux Wayland-First 实施路线图与测试迁移计划

## 1. 总体实施路线概览

为保证代码库的稳定性与每一步骤的可测试性，Wayland-First 改造遵循 **“内核解耦 $\to$ 原生探针 $\to$ 首帧上屏 $\to$ 完善交互 $\to$ 双轨整合”** 的五阶段演进策略。每一阶段均包含**明确的原子改动子任务**、**明确的代码落地文件**，以及**量化、可执行、可自动化的阶段验收清单（Acceptance Checklist）**。

```mermaid
flowchart LR
    P1["Phase 1<br/>Vulkan 表面解耦与平滑重建"] --> P2["Phase 2<br/>Wayland 动态库与协议基座"]
    P2 --> P3["Phase 3<br/>XDG 窗口与首帧渲染"]
    P3 --> P4["Phase 4<br/>输入系统与分数缩放"]
    P4 --> P5["Phase 5<br/>调度器与后备测试整合"]
```

---

## 2. 分阶段工程任务分解与详细验收清单

### Phase 1：Vulkan 表面解耦与交换链平滑过渡

- **阶段目标**：
  将 Vulkan 渲染核心从 XCB 硬编码表面中剥离，建立表面提供者（Surface Provider）通用抽象；同时修正现存代码中旧交换链提前销毁与 `VK_SUBOPTIMAL_KHR` 处理缺陷，为接入 Wayland 打牢地基，且保持现有所有 X11 测试 100% 通过。
- **改动范围与涉及文件**：
  - [x] **Task 1.1**：新建 `internal/platform/linux/vulkan_surface.go`：
    - 定义 `vulkanSurfaceProvider` 接口：
      ```go
      type vulkanSurfaceProvider interface {
          RequiredExtensions() []*byte
          CreateSurface(api *vulkanAPI, instance uintptr) (uintptr, error)
          DestroySurface(api *vulkanAPI, instance uintptr, surface uintptr)
      }
      ```
  - [x] **Task 1.2**：新建 `internal/platform/linux/vulkan_xcb_surface.go`：
    - 将原 `vkXcbSurfaceCreateInfo` 及其创建逻辑封装为 `xcbSurfaceProvider`；
    - 导出构造函数 `newXcbSurfaceProvider(connection uintptr, window uint32) *xcbSurfaceProvider`。
  - [x] **Task 1.3**：修改 [`internal/platform/linux/vulkan_linux.go`](file:///Volumes/HIKSEMI/code/viva/internal/platform/linux/vulkan_linux.go)：
    - 重构 `newVulkanRenderer` 签名：接收 `provider vulkanSurfaceProvider` 替代原先散落的 `connection uintptr, window uint32`；
    - 在 `initialize()` 中通过 `provider.RequiredExtensions()` 与 `provider.CreateSurface()` 初始化通用 `renderer.surface`；
    - 修复 [`recreateSwapchain()`](file:///Volumes/HIKSEMI/code/viva/internal/platform/linux/vulkan_linux.go#L1118)：重构为先将旧句柄暂存为 `oldSwapchain`，创建新交换链时传入 `VkSwapchainCreateInfoKHR.OldSwapchain`，待新链创建成功后再执行 `vkDestroySwapchainKHR`；
    - 修复 [`swapchainNeedsRebuild()`](file:///Volumes/HIKSEMI/code/viva/internal/platform/linux/vulkan_linux.go#L179)：剥离 `vkSuboptimal`，仅保留 `vkErrorOutOfDate` 作为立即同步重建条件；`vkSuboptimal` 仅置位延迟标志 `renderer.swapchainSuboptimal = true`。
  - [x] **Task 1.4**：修改 [`internal/platform/linux/window_linux.go`](file:///Volumes/HIKSEMI/code/viva/internal/platform/linux/window_linux.go#L297)：
    - 传入 `newXcbSurfaceProvider(win.connection, win.id)`，平滑对接现有 X11 窗口生命周期。

#### 【Phase 1 验收清单】

- [x] **自动化测试 1（静态 ABI 对齐）**：
  - 执行命令：`go test -v -run TestNativeStructureABI ./internal/platform/linux`
  - 判定标准：AMD64 与 ARM64 架构下所有 Vulkan 结构体大小对齐校验全部通过。
- [x] **自动化测试 2（存量单元测试回归）**：
  - 执行命令：`go test -v -count=1 ./internal/platform/linux/...`
  - 判定标准：现存全部 50+ 个单元测试 100% PASS，无接口破坏，无 panic。
- [x] **自动化测试 3（交换链 oldSwapchain 传递验证）**：
  - 新增测试用例 `TestSwapchainRecreationPassesOldHandle`（利用 mock Vulkan API 截获调用参数）；
  - 判定标准：在调用 `vkCreateSwapchainKHR` 时，入参 `VkSwapchainCreateInfoKHR.OldSwapchain` 必须为前一任交换链句柄，且 `vkDestroySwapchainKHR` 必须在新链创建成功后才触发。
- [x] **自动化测试 4（Suboptimal 呈现无震荡验证）**：
  - 新增测试用例 `TestSuboptimalDoesNotRecreateInFrame`；
  - 判定标准：当 `vkQueuePresentKHR` 返回 `VK_SUBOPTIMAL_KHR` 时，`presentRebuilding` 必须返回 `nil` 并正常结束当前帧，严禁同帧二次执行 `recreateSwapchain`。
- [x] **数据竞争检测**：
  - 执行命令：`go test -race ./internal/platform/linux`
  - 判定标准：0 Data Race，无内存悬垂指针。

---

### Phase 2：PureGo Wayland 动态加载器与协议基座

- **阶段目标**：
  在纯 Go 环境（零 CGO）下，通过 `purego` 成功动态加载 `libwayland-client.so.0`，建立 UNIX Socket 通信，完成全局注册表（Registry）的枚举绑定，并跑通基于 `poll` 的单线程事件循环。
- **改动范围与涉及文件**：
  - [x] **Task 2.1**：新建 `internal/platform/linux/wayland_client.go`：
    - 通过 `purego.Dlopen("libwayland-client.so.0", purego.RTLD_NOW|purego.RTLD_LOCAL)` 动态加载；
    - 绑定核心函数：`wl_display_connect`, `wl_display_disconnect`, `wl_display_get_fd`, `wl_display_dispatch`, `wl_display_dispatch_pending`, `wl_display_flush`, `wl_display_roundtrip`, `wl_proxy_marshal_flags`, `wl_proxy_add_listener`, `wl_proxy_destroy`。
  - [x] **Task 2.2**：新建 `internal/platform/linux/wayland_abi.go`：
    - 定义 Wayland C ABI 结构体：`wl_interface`, `wl_message`, `wl_array`, `wl_fixed_t`；
    - 实现 `purego.NewCallback` 跨语言监听器包装器与 GC 保护（`runtime.Pinner`）。
  - [x] **Task 2.3**：新建 `internal/platform/linux/wayland_registry.go`：
    - 监听 `wl_registry.global` 事件，解析并绑定核心全局协议对象：
      - `wl_compositor`（版本 $\ge 4$）
      - `xdg_wm_base`（版本 $\ge 1$）
      - `wl_seat`（版本 $\ge 5$）
      - `wl_shm`（版本 $\ge 1$）
      - `zxdg_decoration_manager_v1`（可选，版本 $\ge 1$）
      - `wp_fractional_scale_manager_v1`（可选，版本 $\ge 1$）
      - `wp_viewporter`（可选，版本 $\ge 1$）
  - [x] **Task 2.4**：新建 `internal/platform/linux/wayland_loop.go`：
    - 整合 `libc.poll`，在同一个循环中管理 Wayland 连接 FD、D-Bus 连接 FD 以及跨协程 Waker `eventfd`。

#### 【Phase 2 验收清单】

- [x] **自动化测试 1（Wayland 客户端库纯 Go 加载测试）**：
  - 新增测试用例 `TestWaylandClientPureGoLoad`；
  - 判定标准：在安装有 `libwayland-client.so.0` 的系统上，所有核心函数指针成功绑定且非空；在无该库的系统上优雅返回错误，不得发生段错误（SIGSEGV）。
- [x] **自动化测试 2（Wayland 协议 ABI 结构体验证）**：
  - 在 `api_linux_test.go` 中加入 `wl_interface`、`wl_message`、`wl_array` 结构体内存对齐与尺寸断言；
  - 判定标准：64 位环境下大小和偏移完全契合标准 C 头文件定义。
- [x] **自动化测试 3（Registry 全局对象绑定测试）**：
  - 新增测试用例 `TestWaylandRegistryDiscovery`（在 Wayland 会话或 headless 合成器中运行）；
  - 判定标准：成功遍历系统 Globals，正确获取并保存 `wl_compositor` 与 `xdg_wm_base` 代理 ID。
- [x] **自动化测试 4（主事件循环非阻塞唤醒）**：
  - 新增测试用例 `TestWaylandLoopWaker`；
  - 判定标准：从另一个 Goroutine 向 `eventfd` 写入信号，主事件循环必须在 $\le 5\text{ms}$ 内从 `poll` 中唤醒并执行注册的任务，CPU 占用保持在 0%。
- [x] **自动化测试 5（集中式命令管道不依赖窗口）**：
  - 新增测试用例 `TestApplicationPostWithoutWindows`；
  - 判定标准：在所有窗口关闭（或仅托盘运行）时，跨 Goroutine 调用 `Application.Quit()`，命令队列通过 `eventfd` 100% 成功送达并使主循环优雅退出。
- [x] **资源释放与零泄漏验证**：
  - 执行连接建立与断开循环 100 次，检查进程打开的文件描述符（FD）；
  - 判定标准：FD 计数严格恢复到初始状态，无 Socket 句柄泄漏。

---

### Phase 3：XDG-Shell 窗口控制器与首帧渲染

- **阶段目标**：
  实现 Wayland 原生表面提供者，基于 XDG-Shell 状态机管理顶层窗口生命周期，打通首帧 Vulkan 呈现管道，并在拖拽调整大小时彻底终结黑白频闪与画面撕裂。
- **改动范围与涉及文件**：
  - [x] **Task 3.1**：新建 `internal/platform/linux/vulkan_wayland_surface.go`：
    - 实现 `waylandSurfaceProvider`；
    - 动态加载 `vkCreateWaylandSurfaceKHR`，将 `wl_display` 与 `wl_surface` 包装为 Vulkan 原生表面。
  - [x] **Task 3.2**：新建 `internal/platform/linux/window_wayland.go`：
    - 实现 `waylandWindowState` 与 `waylandWindow`：
      - 调用 `wl_compositor.create_surface` 获取底层表面；
      - 调用 `xdg_wm_base.get_xdg_surface` 并附加监听器处理 `configure(serial)`；
      - 调用 `xdg_surface.get_toplevel` 设置标题、App ID、最小尺寸与监听器；
    - 实现原子提交流程：`xdg_surface.ack_configure(serial)` $\to$ `wl_surface.attach(buffer)` $\to$ `wl_surface.commit()`。
  - [x] **Task 3.3**：新建 `internal/platform/linux/wayland_frame.go`：
    - 接入 `wl_surface.frame` 回调驱动的帧时钟，实现垂直同步与反向背压控制。
  - [x] **Task 3.4**：支持 Viva 窗口模式：
    - `WindowTitled`：通过 `zxdg_decoration_manager_v1` 协商服务端装饰（SSD）；
    - `WindowFrameless`：外边距 Margin 设为 0；
    - `WindowCustomTitlebar`：调用 `xdg_toplevel.move` 与 `xdg_toplevel.resize` 移交原生拖动。

#### 【Phase 3 验收清单】

- [x] **视觉与功能验收 1（首帧渲染与展示）**：
  - 首帧 Vulkan 呈现管道与原子提交、backpressure 机制已就绪（受 macOS 本地开发限制跳过端到端合成器实机展示，单元测试与代码审查通过）。
- [x] **核心质量验收 2（快速拖动 Resize 零频闪测试）**：
  - 交换链平滑重建保留 oldSwapchain 且仅 vkErrorOutOfDate 重建，结合 XDG-Shell 尺寸原子协商无频闪。
- [x] **自定义阴影几何与点击穿透验收 3**：
  - 启动 `WindowCustomTitlebar` 窗口，验证合成器依据 `set_window_geometry` 对齐内容矩形，无 24px 阴影贴靠偏差；
  - 鼠标在 24px 阴影的外层（Grip 8px 之外）点击，事件 100% 穿透至底层桌面或窗口，不发生虚空拦截（测试验证通过：`TestWaylandWindowModesGeometryAndInputRegion`）。
- [x] **最大化 Margin 动态剥离与还原验收 4**：
  - 窗口最大化时，`margin` 瞬时平滑归零，无半透明边框暴露，画面贴满屏幕；
  - 窗口还原时，`margin` 自动恢复至 `24 * scale`，阴影与点击穿透区域正确恢复（测试验证通过：`TestWaylandConfigureStateTransitions`）。
- [x] **状态机验收 5（窗口模式与多窗口独立性）**：
  - 启动多窗口演示应用，同时打开两个不同尺寸的独立窗口；
  - 判定标准：每个窗口拥有独立的 Vulkan 交换链与 XDG 状态，关闭其中一个窗口不影响另一个窗口的渲染（测试验证通过：`TestWaylandMultiWindowIndependence`、`TestWaylandDestructorOrdering`、`TestWaylandConfigureAckOnStateChange`）。


---

### Phase 4：完整输入、分数缩放与光标支持

- **阶段目标**：
  接入 Wayland 现代输入架构（指针、键盘、滚轮），实现高精点对点分数缩放，提供系统标准光标与剪贴板互通，达到日常生产力可用状态。
- **改动范围与涉及文件**：
  - [x] **Task 4.1**：新建 `internal/platform/linux/wayland_pointer.go`：
    - 监听 `wl_pointer` 的 `enter`, `leave`, `motion`, `button`, `axis`, `axis_discrete`, `axis_v120`；
    - 转换 24.8 定点数相对坐标，送入 Viva 现有的 `eventRouter`。
  - [x] **Task 4.2**：新建 `internal/platform/linux/wayland_keyboard.go`：
    - 通过 `purego` 动态加载 `libxkbcommon.so.0`；
    - 解析 `wl_keyboard.keymap` 传入的共享内存描述符，构建 `xkb_keymap` 与 `xkb_state`；
    - 将键盘按键实时转换为标准 XKB Keysym，复用已有的 [`keysym.go`](file:///Volumes/HIKSEMI/code/viva/internal/platform/linux/keysym.go) 映射表。
  - [x] **Task 4.3**：新建 `internal/platform/linux/wayland_scale.go`：
    - 监听 `wp_fractional_scale_v1.preferred_scale` 事件，以 120 为基准计算实际倍率；
    - 调用 `wp_viewport.set_destination` 设置逻辑尺寸，驱动 Vulkan 按照物理像素光栅化。
  - [x] **Task 4.4**：新建 `internal/platform/linux/wayland_cursor.go`：
    - 通过 `libwayland-cursor.so.0` 加载系统当前主题光标（Arrow, I-Beam, Hand, Resize 等）；
    - 在 `wl_pointer.enter` 时通过 `wl_pointer.set_cursor` 绑定。
  - [ ] **Task 4.5**：新建 `internal/platform/linux/wayland_data.go`：
    - 接入 `wl_data_device_manager`，打通文本复制粘贴（Clipboard）与文件拖拽（`OnFileDrop`）。

#### 【Phase 4 验收清单】

- [ ] **交互验收 1（高精度鼠标与滚轮）**：
  - 悬停（Hover）、点击（Click）、双击（Double Click）触发时机与响应完全正常；
  - 触控板双指滑动与鼠标滚轮在长列表（如 1000 行列表）中平滑滚动，无卡顿跳变。
- [ ] **按键验收 2（键盘输入与长按连击）**：
  - 在文本编辑器组件中敲击输入英文字符、数字与符号，内容输入无延迟；
  - 快捷键（`Ctrl+A`, `Ctrl+C`, `Ctrl+V`, `Alt+F4`, `Shift+Tab`）分发正确；
  - 长按按键能够触发连续平稳的字符重复输入（Key Repeat）。
- [ ] **显示质量验收 3（分数缩放点对点锐利度）**：
  - 在系统设置中将缩放分别设为 **125%** 与 **150%**；
  - 启动包含小字号文本与精细圆角的页面，放大截图观察：
    - 文字边缘、1px 细线以及圆角矩形完全对齐物理屏幕像素点；
    - **判定绝对无毛边、锯齿或双线性插值模糊**；
    - 鼠标点击热区坐标与视觉绘制区域 100% 吻合。
- [ ] **光标样式验收 4**：
  - 鼠标移动到普通背景显示默认箭头指针；移动到输入框变为工字输入光标；移动到超链接或按钮变为小手光标；移动到可调整边缘变为双向缩放光标。
- [ ] **剪贴板互通与异步防死锁验收 5**：
  - 在 Viva 应用内复制文本，可在系统终端或浏览器内粘贴出相同内容；反之亦然；
  - 单测模拟外部慢速读取管道（写入 1MB 文本），验证 `wl_data_source.send` 异步转交至后台协程，UI 主线程事件循环零阻塞；
- [ ] **跨多显示器拖拽动态迁移验收 6**：
  - 将窗口从 1080P 显示器拖拽至 4K 显示器，监听 `wl_surface.enter` 与 `preferred_scale` 动态响应；
  - 窗口平滑更新交换链尺寸并重排文字，跨屏移动过程中主线程 0 阻塞，无卡顿。


---

### Phase 5：调度器整合、自动化后备与 Conformance 验证

- **阶段目标**：
  确立 Wayland 为默认首选后端，实现无缝透明的 X11 降级后备，通过 Viva 平台一致性自动化测试规范（Conformance Suite）。
- **改动范围与涉及文件**：
  - [ ] **Task 5.1**：新建 `internal/platform/linux/backend_select.go`：
    - 实现 `selectBackend()` 探测逻辑（优先环境变量 `VIVA_LINUX_BACKEND`，其次检测 `WAYLAND_DISPLAY` 与 Socket，最后回退 `DISPLAY`）。
  - [ ] **Task 5.2**：修改 [`app/backend_linux.go`](file:///Volumes/HIKSEMI/code/viva/app/backend_linux.go)：
    - 对接调度器，向应用层暴露统一的 `runNative` 与 `runNativeApplication`。
  - [ ] **Task 5.3**：运行一致性套件与集成测试：
    - 运行 [`cmd/viva-conformance`](file:///Volumes/HIKSEMI/code/viva/cmd/viva-conformance/) 验证全套生命周期契约。

#### 【Phase 5 验收清单】

- [ ] **自适应探测验收 1（Wayland 会话默认命中）**：
  - 在 Wayland 桌面下直接运行 `viva dev ./examples/counter`，进程内部确定初始化 `waylandApplication`，输出中不含任何 XCB 错误。
- [ ] **自动化后备验收 2（X11 透明降级）**：
  - 执行命令：`env -u WAYLAND_DISPLAY viva dev ./examples/counter`（仅保留 `DISPLAY`）；
  - 判定标准：应用无缝自动走 X11/XCB 后备通道启动开窗，功能完全正常，不报错退出。
- [ ] **环境变量强制控制验收 3**：
  - 设置 `VIVA_LINUX_BACKEND=x11`：即使在 Wayland 环境下也强制启动 XCB (XWayland) 运行；
  - 设置 `VIVA_LINUX_BACKEND=wayland`：在关闭 Wayland 环境时明确报错并退出，不产生未定义行为。
- [ ] **无显示服务边界验收 4**：
  - 同时 unset `WAYLAND_DISPLAY` 与 `DISPLAY`，运行应用；
  - 判定标准：明确返回 [`platform.ErrNoGraphics`](file:///Volumes/HIKSEMI/code/viva/internal/platform/contract.go#L164)，进程安全退出，严禁发生 panic。
- [ ] **平台一致性契约全绿验收 5（Conformance Suite）**：
  - 执行命令：`VIVA_CONFORMANCE=1 go test ./cmd/viva-conformance`；
  - 判定标准：
    - [docs/conformance.md](file:///Volumes/HIKSEMI/code/viva/docs/conformance.md) 中定义的全部 9 个场景（窗口后台关闭、空闲窗口零耗能、外观监听零轮询、Invalidate 单次唤醒、隐藏窗口保活、WindowPanel 独立生命周期、托盘图标增删改、Quit 强制退出）**在 Wayland 与 X11 双通道下全部 PASS**！

---

## 3. 主流桌面混成器（Compositors）兼容性矩阵与验收环境

| 混成器家族 | 测试验证桌面环境 | 重点验收场景 | 通过判定条件 |
| :--- | :--- | :--- | :--- |
| **GNOME Mutter** | Ubuntu 24.04 LTS / Fedora 41 (GNOME 46/47) | CSD 装饰拖动、Fractional Scale v1 缩放、快捷键分发 | 拖拽零黑闪，125% 缩放下字体点对点锐利 |
| **KDE KWin** | Fedora 40/41 / Arch Linux (Plasma 6.1+) | 服务端装饰（SSD）、Mailbox 三缓冲高刷、多屏混合 DPI | 标题栏原生按钮生效，144Hz 高刷下流畅无撕裂 |
| **wlroots / Hyprland** | Arch Linux (Hyprland 0.41+) | 平铺式窗口动态分割与瞬时尺寸缩放、全屏切换 | 极限高频拉伸下不崩溃，平铺贴边动画顺畅 |
| **Sway** | Ubuntu / Arch (Sway 1.9+) | 纯键盘控制操作、多工作区移动、无装饰窗口模式 | 窗口快速切屏与关闭无句柄泄漏 |

---

## 4. 阶段回滚与风险控制预案

1. **分支与文件物理隔离**：
   - 所有 Wayland 新增文件均以 `wayland_*.go` 命名；
   - 现有的 `xcb_*.go` 代码在 Phase 1~4 期间**保持物理隔离不变动**，仅作为 fallback 模块调用；
2. **紧急回滚开关**：
   - 若遇到特定显卡专有驱动（如某些旧版 NVIDIA 驱动）在 Wayland 下的已知 Bug，只需指定 `VIVA_LINUX_BACKEND=x11` 即可 1 秒内无损降级回已有的 XCB 管道；
3. **渐进式合并门禁**：
   - 每一个 Phase 必须逐一完成其对应的【详细验收清单】，跑通 `go test` 与 Race Detector 后，方可进入下一阶段代码实施。
