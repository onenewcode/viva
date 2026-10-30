---
title: 系统集成
summary: 剪贴板、文件对话框、通知、密钥等操作系统能力，以及失败时如何报告。
order: 11
group: guides
---

`app` 包提供一小组同步的系统集成接口。每个操作要么按声明的语义完成，要么返回错误；适配层从不报告它没有做到的成功。

| 能力 | 函数 |
| --- | --- |
| 剪贴板文本 | `app.ClipboardText`、`app.SetClipboardText` |
| 文件对话框 | `app.OpenFiles`、`app.OpenDirectory`、`app.SaveFile` |
| 打开 URL | `app.OpenURL` |
| 用默认应用打开本地文档(绝不运行程序) | `app.OpenPath` |
| 通知与声音 | `app.SendNotification`、`app.PlaySystemSound` |
| 光标 | `app.SetCursor` |
| 系统首选语言 | `app.PreferredLanguages` |
| 系统外观（浅色/深色） | `app.SystemAppearance`、`app.WatchSystemAppearance` |
| 系统绘制部分的外观 | `app.SetNativeAppearance` |
| 减弱动效 | `app.ReduceMotion` |
| 密钥 | `app.StoreSecret`、`app.LoadSecret`、`app.DeleteSecret` |
| 状态栏图标与面板 | `Application.AddStatusItem`、`app.WindowPanel` |
| 等宽字体 | `app.MonospaceFamilies` |
| Dock 角标 | `Application.SetDockBadge` |
| 文件管理器为文件或文件夹显示的图标 | `app.FileIcon` |

## 各平台用什么实现

每个函数都映射到平台自己的接口：macOS 上是 `NSPasteboard`、`NSOpenPanel` 和 `NSWorkspace`；Windows 上是 Win32 剪贴板、`IFileOpenDialog` 和 `ShellExecuteW`；Linux 上是 X11 选区，文件对话框使用 `zenity` 或 `kdialog`，打开 URL 使用 `xdg-open` 或 `gio`，通知使用 `notify-send`。密钥存入钥匙串、凭据管理器或 Secret Service。

完整的表格，包括某个平台做不到某件事的情形，见仓库中的 `docs/system-integrations.md`。

## 错误

调用方用 `errors.Is` 比较，永远不要比较字符串：

| 错误 | 含义 |
| --- | --- |
| `ErrUnsupported` | 当前平台或桌面环境无法提供该能力。 |
| `ErrCanceled` | 用户关闭了文件对话框。 |
| `ErrPermissionDenied` | 用户或系统拒绝了。 |
| `ErrAsyncRequired` | 浏览器只提供 Promise，同步接口不会等待它。 |
| `ErrUserGestureRequired` | 浏览器拒绝了不在直接用户手势中的调用。 |

## 值得了解的边界

- Linux 上的文件对话框、URL 分发和通知，会在用户请求时启动已安装的桌面工具。Viva 不捆绑 GTK、Qt 或 D-Bus 栈。
- 在浏览器里，剪贴板和文件选择器按设计返回 `ErrAsyncRequired`。提前返回会谎报成功，而等待会让 WebAssembly 事件循环死锁。
- 要跟随系统的浅色或深色外观，就在绘制时读取 `app.SystemAppearance`，并只调用一次 `app.WatchSystemAppearance`，传入一个请求重绘的函数。它在事件循环线程上调用，且只在外观真的变了时调用。Linux 不监听桌面门户，而是在窗口获得焦点时重新读取一次，桌面没有给出偏好时返回 `AppearanceUnknown`；无窗口的 `Driver` 在调用 `SetSystemAppearance` 之前也返回 `AppearanceUnknown`。
- 如果用户可以不管系统而固定选择浅色或深色，就用这个选择调用 `app.SetNativeAppearance`，这样 macOS 自己绘制的窗口按钮、菜单、文件对话框和原生文本编辑会和窗口一致；传入 `AppearanceUnknown` 就还给系统，期间 `SystemAppearance` 仍然报告系统自己的外观。
- Dock 角标用 macOS 自己的机制：系统在应用 Dock 图标右上角画一个红色徽标并写入这段文字，尺寸也由系统决定。Windows 和 Linux 没有 Dock 图标，什么也不做；空文字表示去掉角标。
- Linux 剪贴板读取有三秒期限，不增加空闲线程或轮询循环。
