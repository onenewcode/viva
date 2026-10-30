//go:build windows

package windows

// SetDockBadge does nothing: Windows has no Dock tile, and the taskbar badge a
// program can put on its button is a picture (ITaskbarList3.SetOverlayIcon)
// rather than a label, so an application that wants one gives the framework the
// picture to show. No application has needed that yet.
func (application *windowsApplication) SetDockBadge(text string) {}
