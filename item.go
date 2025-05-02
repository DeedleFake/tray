package tray

import (
	"fmt"
	"image"
	"sync/atomic"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

const itemPath dbus.ObjectPath = "/StatusNotifierItem"

type Item struct {
	conn               *dbus.Conn
	props              *prop.Properties
	menu               lazy[*Menu]
	space, inter, name string
	handler            atomic.Pointer[Handler]
}

func New() (*Item, error) {
	conn, err := dbus.ConnectSessionBus()
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
	item.name = getName(space)

	err = item.conn.Export((*statusNotifierItem)(item), itemPath, item.inter)
	if err != nil {
		return err
	}

	err = item.exportProps()
	if err != nil {
		return err
	}

	err = item.exportIntrospect()
	if err != nil {
		return err
	}

	reply, err := item.conn.RequestName(item.name, 0)
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
		item.name,
	).Store()
	if err != nil {
		return err
	}

	return nil
}

func (item *Item) exportProps() error {
	m := prop.Map{
		item.inter: map[string]*prop.Prop{
			"Category":            makeProp(ApplicationStatus),
			"Id":                  makeProp(""),
			"Title":               makeProp(""),
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
			"Menu":                makeConstProp(menuPath),
		},
	}

	props, err := prop.Export(item.conn, itemPath, m)
	if err != nil {
		return err
	}
	item.props = props
	return nil
}

func (item *Item) exportIntrospect() error {
	node := introspect.Node{
		Name: string(itemPath),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			{
				Name:       item.inter,
				Methods:    introspect.Methods((*statusNotifierItem)(item)),
				Properties: item.props.Introspection(item.inter),
				Signals: []introspect.Signal{
					{Name: "NewTitle"},
					{Name: "NewIcon"},
					{Name: "NewAttentionIcon"},
					{Name: "NewOverlayIcon"},
					{Name: "NewToolTip"},
					{Name: "NewStatus"},
				},
			},
		},
	}

	return item.conn.Export(introspect.NewIntrospectable(&node), itemPath, "org.freedesktop.DBus.Introspectable")
}

func (item *Item) Close() error {
	reply, err := item.conn.ReleaseName(item.name)
	if err != nil {
		return err
	}
	if reply != dbus.ReleaseNameReplyReleased {
		return fmt.Errorf("bad reply to name release: %v", reply)
	}

	return nil
}

func (item *Item) emit(name string) error {
	return item.conn.Emit(itemPath, fmt.Sprintf("org.%v.StatusNotifierItem.%v", item.space, name))
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

func (item *Item) SetID(id string) {
	item.props.SetMust(item.inter, "Id", id)
}

func (item *Item) Title() string {
	return item.props.GetMust(item.inter, "Title").(string)
}

func (item *Item) SetTitle(title string) error {
	item.props.SetMust(item.inter, "Title", title)
	return item.emit("NewTitle")
}

func (item *Item) Status() Status {
	return item.props.GetMust(item.inter, "Status").(Status)
}

func (item *Item) SetStatus(status Status) error {
	item.props.SetMust(item.inter, "Status", status)
	return item.emit("NewStatus")
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
	return item.emit("NewIcon")
}

func (item *Item) IconPixmap() []image.Image {
	pixmaps := item.props.GetMust(item.inter, "IconPixmap").([]pixmap)
	return fromPixmaps(pixmaps)
}

func (item *Item) SetIconPixmap(images ...image.Image) error {
	pixmaps := toPixmaps(images)
	item.props.SetMust(item.inter, "IconPixmap", pixmaps)
	return item.emit("NewIcon")
}

func (item *Item) OverlayIconName() string {
	return item.props.GetMust(item.inter, "OverlayIconName").(string)
}

func (item *Item) SetOverlayIconName(name string) error {
	item.props.SetMust(item.inter, "OverlayIconName", name)
	return item.emit("NewOverlayIcon")
}

func (item *Item) OverlayIconPixmap() []image.Image {
	pixmaps := item.props.GetMust(item.inter, "OverlayIconPixmap").([]pixmap)
	return fromPixmaps(pixmaps)
}

func (item *Item) SetOverlayIconPixmap(images ...image.Image) error {
	pixmaps := toPixmaps(images)
	item.props.SetMust(item.inter, "OverlayIconPixmap", pixmaps)
	return item.emit("NewOverlayIcon")
}

func (item *Item) AttentionIconPixmap() []image.Image {
	pixmaps := item.props.GetMust(item.inter, "AttentionIconPixmap").([]pixmap)
	return fromPixmaps(pixmaps)
}

func (item *Item) SetAttentionIconPixmap(images ...image.Image) error {
	pixmaps := toPixmaps(images)
	item.props.SetMust(item.inter, "AttentionIconPixmap", pixmaps)
	return item.emit("NewAttentionIcon")
}

func (item *Item) AttentionMovieName() string {
	return item.props.GetMust(item.inter, "AttentionMovieName").(string)
}

func (item *Item) SetAttentionMovieName(name string) {
	item.props.SetMust(item.inter, "AttentionMovieName", name)
}

func (item *Item) ToolTip() (iconName string, iconPixmap []image.Image, title, description string) {
	tooltip := item.props.GetMust(item.inter, "ToolTip").(tooltip)
	return tooltip.IconName, fromPixmaps(tooltip.IconPixmap), tooltip.Title, tooltip.Description
}

func (item *Item) SetToolTip(iconName string, iconPixmap []image.Image, title, description string) error {
	item.props.SetMust(item.inter, "ToolTip", tooltip{IconName: iconName, IconPixmap: toPixmaps(iconPixmap), Title: title, Description: description})
	return item.emit("NewToolTip")
}

func (item *Item) ItemIsMenu() bool {
	return item.props.GetMust(item.inter, "ItemIsMenu").(bool)
}

func (item *Item) SetItemIsMenu(itemIsMenu bool) {
	item.props.SetMust(item.inter, "ItemIsMenu", itemIsMenu)
}

func (item *Item) Handler() Handler {
	h := item.handler.Load()
	if h == nil {
		return nil
	}
	return *h
}

func (item *Item) SetHandler(handler Handler) {
	p := &handler
	if handler == nil {
		p = nil
	}
	item.handler.Store(p)
}

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
