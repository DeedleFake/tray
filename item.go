package tray

import (
	"fmt"
	"image"
	"image/draw"

	"deedles.dev/ximage/format"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
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

type Item struct {
	conn         *dbus.Conn
	sni          dbus.BusObject
	props        *prop.Properties
	space, inter string
}

func New() (*Item, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, err
	}

	item := Item{
		conn: conn,
	}
	err = item.export()
	if err != nil {
		return nil, err
	}

	return &item, nil
}

func (item *Item) export() error {
	space, err := getSpace(item.conn)
	if err != nil {
		return err
	}
	item.space = space
	item.inter = fmt.Sprintf("org.%v.StatusNotifierItem", space)

	name := getName(space)
	item.sni = item.conn.Object(name, "/StatusNotifierItem")

	err = item.conn.Export(
		statusNotifierItem{item: item},
		item.sni.Path(),
		item.sni.Destination(),
	)
	if err != nil {
		return err
	}

	props, err := prop.Export(item.conn, item.sni.Path(), makePropMap(item.inter))
	if err != nil {
		return err
	}
	item.props = props

	err = exportIntrospect(
		item.conn,
		item.inter,
		item.sni,
		props,
	)
	if err != nil {
		return err
	}

	reply, err := item.conn.RequestName(item.sni.Destination(), 0)
	if err != nil {
		return err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("bad reply to name request: %v", reply)
	}

	err = item.conn.Object(
		fmt.Sprintf("org.%v.StatusNotifierWatcher", space),
		"/StatusNotifierWatcher",
	).Call(
		fmt.Sprintf("org.%v.StatusNotifierWatcher.RegisterStatusNotifierItem", space),
		0,
		item.sni.Destination(),
	).Store()
	if err != nil {
		return err
	}

	return nil
}

func (item *Item) Close() error {
	reply, err := item.conn.ReleaseName(item.sni.Destination())
	if err != nil {
		return err
	}
	if reply != dbus.ReleaseNameReplyReleased {
		return fmt.Errorf("bad reply to name release: %v", reply)
	}

	return nil
}

func (item *Item) Category() Category {
	return item.props.GetMust(item.inter, "Category").(Category)
}

func (item *Item) SetCategory(category Category) {
	item.props.SetMust(item.inter, "Category", category)
}

func (item *Item) ID() string {
	return item.props.GetMust(item.inter, "Id").(string)
}

func (item *Item) SetID(id string) error {
	return item.sni.SetProperty("Id", id)
}

func (item *Item) Status() Status {
	return item.props.GetMust(item.inter, "Status").(Status)
}

func (item *Item) SetStatus(status Status) {
	item.props.SetMust(item.inter, "Status", status)
}

func (item *Item) WindowID() uint32 {
	return item.props.GetMust(item.inter, "WindowId").(uint32)
}

func (item *Item) SetWindowID(id uint32) {
	item.props.SetMust(item.inter, "WindowId", id)
}

func (item *Item) IconName() string {
	return item.props.GetMust(item.inter, "IconName").(string)
}

func (item *Item) SetIconName(name string) error {
	item.props.SetMust(item.inter, "IconName", name)
	return item.conn.Emit(item.sni.Path(), fmt.Sprintf("org.%v.StatusNotifierItem.NewIcon", item.space))
}

func (item *Item) IconPixmap() []image.Image {
	pixmaps := item.props.GetMust(item.inter, "IconPixmap").([]pixmap)
	images := make([]image.Image, 0, len(pixmaps))
	for _, p := range pixmaps {
		images = append(images, p.Image())
	}
	return images
}

func (item *Item) SetIconPixmap(images ...image.Image) error {
	pixmaps := make([]pixmap, 0, len(images))
	for _, img := range images {
		pixmaps = append(pixmaps, toPixmap(img))
	}
	item.props.SetMust(item.inter, "IconPixmap", pixmaps)
	return item.conn.Emit(item.sni.Path(), fmt.Sprintf("org.%v.StatusNotifierItem.NewIcon", item.space))
}

func makePropMap(inter string) prop.Map {
	m := make(prop.Map, 1)
	m[inter] = map[string]*prop.Prop{
		"Category":            makeProp(ApplicationStatus),
		"Id":                  makeProp(""),
		"Status":              makeProp(Active),
		"WindowId":            makeProp(uint32(0)),
		"IconName":            makeProp(""),
		"IconPixmap":          makeProp[[]pixmap](nil),
		"OverlayIconName":     makeProp(""),
		"OverlayIconPixmap":   makeProp[[]pixmap](nil),
		"AttentionIconName":   makeProp(""),
		"AttentionIconPixmap": makeProp[[]pixmap](nil),
		"AttentionMovieName":  makeProp(""),
		"ToolTip":             makeProp(tooltip{}),
		"ItemIsMenu":          makeProp(false),
		"Menu":                makeProp[dbus.ObjectPath]("/"),
	}
	return m
}

func makeProp[T any](v T) *prop.Prop {
	return &prop.Prop{
		Value: v,
		Emit:  prop.EmitTrue,
	}
}

func exportIntrospect(conn *dbus.Conn, inter string, obj dbus.BusObject, props *prop.Properties) error {
	path := obj.Path()
	node := introspect.Node{
		Name: string(path),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			{
				Name:       inter,
				Methods:    introspect.Methods(statusNotifierItem{}),
				Properties: props.Introspection(inter),
				Signals: []introspect.Signal{
					{Name: "NewIcon"},
				},
			},
		},
	}

	return conn.Export(introspect.NewIntrospectable(&node), path, "org.freedesktop.DBus.Introspectable")
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

	return pixmap{
		Width:  bounds.Dx(),
		Height: bounds.Dy(),
		Data:   dst.Pix,
	}
}

func (p pixmap) Image() image.Image {
	return &format.Image{
		Format: format.ARGB8888,
		Rect:   image.Rect(0, 0, p.Width, p.Height),
		Pix:    p.Data,
	}
}

type tooltip struct {
	IconName           string
	IconPixmap         []pixmap
	Title, Description string
}
