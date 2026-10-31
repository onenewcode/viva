// Package dmg makes the disk image an application is distributed in: the
// bundle and a shortcut to /Applications, laid out in the window the Finder
// opens so that installing is one drag.
//
// A disk image carries no layout of its own. The Finder remembers the window
// size, background picture and icon positions of a folder in a .DS_Store file
// inside it, which this package writes directly, so no Finder window has to
// be opened and scripted to make one.
package dmg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image/png"
	"math"
	"os"
)

const (
	// DefaultWidth and DefaultHeight are the window's content size in points
	// when there is no background picture to set it.
	DefaultWidth, DefaultHeight = 540, 380
	// DefaultIconSize is the size of the icons in points.
	DefaultIconSize = 128

	// titleBarHeight is what the window adds above its content when the
	// toolbar is hidden; the Finder's bounds include it.
	titleBarHeight = 28
	// The Finder stores a window's position from the bottom left of the screen.
	windowX, windowBottom = 200, 400
)

// Point is a position in points from the top left of the window's content, at
// the middle of an icon.
type Point struct{ X, Y int }

// Config is what a project says about its disk image. Every field may be left
// at its zero value.
type Config struct {
	// Background is a PNG shown behind the icons. Its size in points, its pixel
	// size over the scale its resolution says (144 dpi is 2x), is the window's
	// content size. Without one the image shows a plain ground with an arrow
	// from the application to the Applications shortcut.
	Background string
	// IconSize is the icons' size in points.
	IconSize int
	// App and Applications place the two icons. They default to either side of
	// the middle of the window.
	App, Applications *Point
}

// Layout is Config with every default applied.
type Layout struct {
	Background        string
	Width, Height     int
	IconSize          int
	App, Applications Point
}

// Resolve applies the defaults and checks the background picture.
func Resolve(config Config) (Layout, error) {
	layout := Layout{Background: config.Background, Width: DefaultWidth, Height: DefaultHeight, IconSize: config.IconSize}
	if config.Background != "" {
		data, err := os.ReadFile(config.Background)
		if err != nil {
			return Layout{}, err
		}
		width, height, err := pictureSize(config.Background, data)
		if err != nil {
			return Layout{}, err
		}
		layout.Width, layout.Height = width, height
	}
	if layout.IconSize == 0 {
		layout.IconSize = DefaultIconSize
	}
	layout.App = Point{layout.Width/2 - 140, layout.Height/2 - 15}
	layout.Applications = Point{layout.Width/2 + 140, layout.Height/2 - 15}
	if config.App != nil {
		layout.App = *config.App
	}
	if config.Applications != nil {
		layout.Applications = *config.Applications
	}
	return layout, nil
}

// pictureSize is the size in points of a PNG, from its pixels and the
// resolution it is tagged with. An untagged picture is 1x.
func pictureSize(path string, data []byte) (int, int, error) {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, fmt.Errorf("dmg: background %s is not a PNG: %w", path, err)
	}
	scale := 1
	// Chunks follow the 8-byte signature as length, type, data, checksum.
	for rest := data[8:]; len(rest) >= 12; {
		length := int(binary.BigEndian.Uint32(rest))
		if length < 0 || 12+length > len(rest) {
			break
		}
		if string(rest[4:8]) == "pHYs" && length == 9 {
			x, y, unit := binary.BigEndian.Uint32(rest[8:]), binary.BigEndian.Uint32(rest[12:]), rest[16]
			if unit == 1 && x == y {
				dpi := float64(x) * 0.0254
				scale = int(math.Round(dpi / 72))
				if scale < 1 || scale > 3 || math.Abs(dpi-float64(scale*72)) > 1 {
					return 0, 0, fmt.Errorf("dmg: background %s is %.0f dpi; tag it 72 dpi (1x), 144 (2x) or 216 (3x)", path, dpi)
				}
			}
			break
		}
		rest = rest[12+length:]
	}
	if config.Width%scale != 0 || config.Height%scale != 0 {
		return 0, 0, fmt.Errorf("dmg: background %s is %dx%d pixels, not a whole number of points at %dx", path, config.Width, config.Height, scale)
	}
	return config.Width / scale, config.Height / scale, nil
}

// settings is the .DS_Store of the volume's root folder. alias is the
// background picture's alias record.
func (l Layout) settings(app string, alias []byte) ([]byte, error) {
	bounds := fmt.Sprintf("{{%d, %d}, {%d, %d}}", windowX, windowBottom, l.Width, l.Height+titleBarHeight)
	window, err := encodeBinaryPlist([]plistEntry{
		plistPair("ShowStatusBar", plistBoolean(false)),
		plistPair("WindowBounds", plistText(bounds)),
		plistPair("ContainerShowSidebar", plistBoolean(false)),
		plistPair("PreviewPaneVisibility", plistBoolean(false)),
		plistPair("SidebarWidth", plistInteger(180)),
		plistPair("ShowTabView", plistBoolean(false)),
		plistPair("ShowToolbar", plistBoolean(false)),
		plistPair("ShowPathbar", plistBoolean(false)),
		plistPair("ShowSidebar", plistBoolean(false)),
	})
	if err != nil {
		return nil, err
	}
	view := []plistEntry{
		plistPair("viewOptionsVersion", plistInteger(1)),
		plistPair("backgroundType", plistInteger(0)),
		plistPair("backgroundColorRed", plistFloat(1)),
		plistPair("backgroundColorGreen", plistFloat(1)),
		plistPair("backgroundColorBlue", plistFloat(1)),
		plistPair("gridOffsetX", plistFloat(0)),
		plistPair("gridOffsetY", plistFloat(0)),
		plistPair("gridSpacing", plistFloat(100)),
		plistPair("arrangeBy", plistText("none")),
		plistPair("showIconPreview", plistBoolean(true)),
		plistPair("showItemInfo", plistBoolean(false)),
		plistPair("labelOnBottom", plistBoolean(true)),
		plistPair("textSize", plistFloat(12)),
		plistPair("iconSize", plistFloat(float64(l.IconSize))),
		plistPair("scrollPositionX", plistFloat(0)),
		plistPair("scrollPositionY", plistFloat(0)),
	}
	view[1] = plistPair("backgroundType", plistInteger(2))
	view = append(view, plistPair("backgroundImageAlias", plistBytes(alias)))
	icons, err := encodeBinaryPlist(view)
	if err != nil {
		return nil, err
	}
	place := func(name string, at Point) dsRecord {
		position := binary.BigEndian.AppendUint32(nil, uint32(at.X))
		position = binary.BigEndian.AppendUint32(position, uint32(at.Y))
		return dsBlob(name, "Iloc", append(position, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x00, 0x00))
	}
	if app == "" {
		return nil, errors.New("dmg: the application has no name")
	}
	return encodeDSStore([]dsRecord{
		dsBlob(".", "bwsp", window),
		dsType(".", "icvl", "icnv"),
		dsBlob(".", "icvp", icons),
		dsLong(".", "vSrn", 1),
		place(app, l.App),
		place("Applications", l.Applications),
	})
}
