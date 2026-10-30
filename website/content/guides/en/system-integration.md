---
title: System integration
summary: Clipboard, file dialogs, notifications, secrets and the rest of the operating system, and how failures are reported.
order: 11
group: guides
---

The `app` package exposes a small, synchronous set of system integrations. An operation either completes with the semantics it declares or returns an error. An adapter never reports a success it did not achieve.

| Capability | Functions |
| --- | --- |
| Clipboard text | `app.ClipboardText`, `app.SetClipboardText` |
| File dialogs | `app.OpenFiles`, `app.OpenDirectory`, `app.SaveFile` |
| Open a URL | `app.OpenURL` |
| Open a local document in its default application (never a program) | `app.OpenPath` |
| Notifications and sounds | `app.SendNotification`, `app.PlaySystemSound` |
| Cursor | `app.SetCursor` |
| Preferred languages | `app.PreferredLanguages` |
| System appearance | `app.SystemAppearance`, `app.WatchSystemAppearance` |
| Look of what the system draws | `app.SetNativeAppearance` |
| Reduced motion | `app.ReduceMotion` |
| Secrets | `app.StoreSecret`, `app.LoadSecret`, `app.DeleteSecret` |
| Status-area icons and panels | `Application.AddStatusItem`, `app.WindowPanel` |
| Fixed-pitch fonts | `app.MonospaceFamilies` |
| Dock badge | `Application.SetDockBadge` |
| The icon the file manager shows for a file or folder | `app.FileIcon` |

## What each platform uses

Each function maps to the platform's own API: `NSPasteboard`, `NSOpenPanel` and `NSWorkspace` on macOS; the Win32 clipboard, `IFileOpenDialog` and `ShellExecuteW` on Windows; X11 selections on Linux, with the desktop portal's file chooser and the notification service over D-Bus (`zenity`, `kdialog` and `notify-send` when a desktop has neither), and `xdg-open` or `gio` for URLs. Secrets go to the Keychain, Credential Manager or Secret Service.

The full table, including the cases where a platform cannot do something, is the repository's `docs/system-integrations.md`.

## Errors

Callers compare with `errors.Is`, never with strings:

| Error | Meaning |
| --- | --- |
| `ErrUnsupported` | This platform or desktop environment cannot provide the capability. |
| `ErrCanceled` | The user dismissed a file dialog. |
| `ErrPermissionDenied` | The user or the system denied it. |
| `ErrAsyncRequired` | The browser only offers a Promise. The synchronous API will not wait for it. |
| `ErrUserGestureRequired` | A browser rejected the call outside a direct user gesture. |

## Boundaries worth knowing

- Linux file dialogs and notifications use the desktop's portal and notification service over D-Bus, and start an installed desktop utility only on a desktop without them; URL dispatch always does. Viva does not bundle GTK or Qt.
- In the browser, the clipboard and file pickers return `ErrAsyncRequired` by design. Returning early would lie about success, and waiting would deadlock the WebAssembly event loop.
- To follow the system's light or dark look, read `app.SystemAppearance` while painting and call `app.WatchSystemAppearance` once with a function that requests a redraw. It is called on the event-loop thread, only when the look really changed. Linux listens to the desktop portal's signal while something watches, falls back to the XSETTINGS manager, `GTK_THEME` and the GTK and KDE configuration files, and answers `AppearanceUnknown` when none gives a preference; a headless `Driver` answers `AppearanceUnknown` until `SetSystemAppearance` says otherwise.
- If the user can pick light or dark regardless of the system, call `app.SetNativeAppearance` with that pick so macOS's own window buttons, menus, file dialogs and native text editing match the window; `AppearanceUnknown` hands them back to the system, and `SystemAppearance` keeps reporting the system's look meanwhile.
- The Dock badge is macOS's own: a label in the red badge at the corner of the application's Dock tile, which the system draws and sizes. Windows and Linux have no Dock tile and do nothing with it; an empty label removes the badge.
- Linux clipboard reads have a three-second deadline and add no idle worker or polling loop.
