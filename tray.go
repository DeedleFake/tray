package tray

import (
	"fmt"
	"image"
	"image/draw"
	"os"
	"slices"
	"sync/atomic"

	"deedles.dev/ximage/format"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

type Category string

const (
	ApplicationStatus Category = "ApplicationStatus"
	Communications    Category = "Communications"
	SystemServices    Category = "SystemServices"
	Hardware          Category = "Hardware"
)

type Status string

const (
	Passive        Status = "Passive"
	Active         Status = "Active"
	NeedsAttention Status = "NeedsAttention"
)

type Orientation string

const (
	Horizontal Orientation = "horizontal"
	Vertical   Orientation = "vertical"
)

var id uint64

func getName(space string) string {
	id := atomic.AddUint64(&id, 1)
	return fmt.Sprintf("org.%v.StatusNotifierItem-%v-%v", space, os.Getpid(), id)
}

func getSpace(conn *dbus.Conn) (string, error) {
	var freedesktopOwned bool
	err := conn.BusObject().Call("NameHasOwner", 0, "org.freedesktop.StatusNotifierWatcher").Store(&freedesktopOwned)
	if err != nil {
		return "", err
	}

	if freedesktopOwned {
		return "freedesktop", nil
	}
	return "kde", nil
}

type pixmap struct {
	Width, Height int
	Data          []byte
}

func toPixmap(img image.Image) pixmap {
	bounds := img.Bounds().Canon()
	dst := &format.Image{
		Format: format.ARGB8888,
		Rect:   bounds,
		Pix:    make([]byte, format.ARGB8888.Size()*bounds.Dx()*bounds.Dy()),
	}
	draw.Draw(dst, bounds, img, bounds.Min, draw.Src)
	endianSwap(dst.Pix)

	return pixmap{
		Width:  bounds.Dx(),
		Height: bounds.Dy(),
		Data:   dst.Pix,
	}
}

func (p pixmap) Image() image.Image {
	data := slices.Clone(p.Data)
	endianSwap(data)

	return &format.Image{
		Format: format.ARGB8888,
		Rect:   image.Rect(0, 0, p.Width, p.Height),
		Pix:    data,
	}
}

func fromPixmaps(pixmaps []pixmap) []image.Image {
	images := make([]image.Image, 0, len(pixmaps))
	for _, p := range pixmaps {
		images = append(images, p.Image())
	}
	return images
}

func toPixmaps(images []image.Image) []pixmap {
	pixmaps := make([]pixmap, 0, len(images))
	for _, img := range images {
		pixmaps = append(pixmaps, toPixmap(img))
	}
	return pixmaps
}

func endianSwap(data []byte) {
	if len(data)%4 != 0 {
		panic(fmt.Errorf("len(data) %% 4 != 0, len(data) == %v", len(data)))
	}

	for i := 0; i < len(data); i += 4 {
		slices.Reverse(data[i : i+4])
	}
}

type tooltip struct {
	IconName           string
	IconPixmap         []pixmap
	Title, Description string
}

func makeProp[T any](v T) *prop.Prop {
	return &prop.Prop{
		Value: v,
		Emit:  prop.EmitTrue,
	}
}
