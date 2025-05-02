package tray

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"slices"
	"sync/atomic"

	"deedles.dev/ximage/format"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

const itemPath dbus.ObjectPath = "/StatusNotifierItem"

type Item struct {
	conn               *dbus.Conn
	props              *prop.Properties
	menu               *Menu
	space, inter, name string
	handler            atomic.Pointer[Handler]
}

func New(props ...ItemProp) (*Item, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}

	item := Item{
		conn: conn,
	}
	err = item.export(props)
	if err != nil {
		return nil, err
	}

	return &item, nil
}

func (item *Item) export(props []ItemProp) error {
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

	err = item.createMenu()
	if err != nil {
		return err
	}

	err = item.SetProps(props...)
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
			"IconAccessibleDesc":  makeProp(""),
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
	return item.conn.Close()
}

func (item *Item) emit(name string) error {
	return item.conn.Emit(itemPath, fmt.Sprintf("org.%v.StatusNotifierItem.%v", item.space, name))
}

func (item *Item) SetProps(props ...ItemProp) error {
	w := itemProps{Item: item}
	for _, p := range props {
		p(&w)
	}

	errs := make([]error, 0, len(w.dirty))
	for _, s := range w.dirty {
		errs = append(errs, item.emit(s))
	}

	return errors.Join(errs...)
}

func (item *Item) Category() Category {
	return item.props.GetMust(item.inter, "Category").(Category)
}

func (item *Item) ID() string {
	return item.props.GetMust(item.inter, "Id").(string)
}

func (item *Item) Title() string {
	return item.props.GetMust(item.inter, "Title").(string)
}

func (item *Item) Status() Status {
	return item.props.GetMust(item.inter, "Status").(Status)
}

func (item *Item) WindowID() uint32 {
	return item.props.GetMust(item.inter, "WindowId").(uint32)
}

func (item *Item) IconName() string {
	return item.props.GetMust(item.inter, "IconName").(string)
}

func (item *Item) IconPixmap() []image.Image {
	pixmaps := item.props.GetMust(item.inter, "IconPixmap").([]pixmap)
	return fromPixmaps(pixmaps)
}

func (item *Item) IconAccessibleDesc() string {
	return item.props.GetMust(item.inter, "IconAccessibleDesc").(string)
}

func (item *Item) OverlayIconName() string {
	return item.props.GetMust(item.inter, "OverlayIconName").(string)
}

func (item *Item) OverlayIconPixmap() []image.Image {
	pixmaps := item.props.GetMust(item.inter, "OverlayIconPixmap").([]pixmap)
	return fromPixmaps(pixmaps)
}

func (item *Item) AttentionIconPixmap() []image.Image {
	pixmaps := item.props.GetMust(item.inter, "AttentionIconPixmap").([]pixmap)
	return fromPixmaps(pixmaps)
}

func (item *Item) AttentionMovieName() string {
	return item.props.GetMust(item.inter, "AttentionMovieName").(string)
}

func (item *Item) ToolTip() (iconName string, iconPixmap []image.Image, title, description string) {
	tooltip := item.props.GetMust(item.inter, "ToolTip").(tooltip)
	return tooltip.IconName, fromPixmaps(tooltip.IconPixmap), tooltip.Title, tooltip.Description
}

func (item *Item) IsMenu() bool {
	return item.props.GetMust(item.inter, "ItemIsMenu").(bool)
}

func (item *Item) Menu() *Menu {
	return item.menu
}

func (item *Item) Handler() Handler {
	h := item.handler.Load()
	if h == nil {
		return nil
	}
	return *h
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

type tooltip struct {
	IconName           string
	IconPixmap         []pixmap
	Title, Description string
}

type ItemProp func(*itemProps)

type itemProps struct {
	*Item
	dirty []string
}

func (item *itemProps) mark(change string) {
	if !slices.Contains(item.dirty, change) {
		item.dirty = append(item.dirty, change)
	}
}

func ItemCategory(category Category) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "Category", category)
	}
}

func ItemID(id string) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "Id", id)
	}
}

func ItemTitle(title string) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "Title", title)
		item.mark("NewTitle")
	}
}

func ItemStatus(status Status) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "Status", status)
		item.mark("NewStatus")
	}
}

func ItemWindowID(id uint32) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "WindowId", id)
	}
}

func ItemIconName(name string) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "IconName", name)
		item.mark("NewIcon")
	}
}

func ItemIconPixmap(images ...image.Image) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "IconPixmap", toPixmaps(images))
		item.mark("NewIcon")
	}
}

func ItemIconAccessibleDesc(desc string) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "IconAccessibleDesc", desc)
		item.mark("NewIcon")
	}
}

func ItemOverlayIconName(name string) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "OverlayIconName", name)
		item.mark("NewOverlayIcon")
	}
}

func ItemOverlayIconPixmap(images ...image.Image) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "OverlayIconPixmap", toPixmaps(images))
		item.mark("NewOverlayIcon")
	}
}

func ItemAttentionIconName(name string) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "AttentionIconName", name)
		item.mark("NewAttentionIcon")
	}
}

func ItemAttentionIconPixmap(images ...image.Image) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "AttentionIconPixmap", toPixmaps(images))
		item.mark("NewAttentionIcon")
	}
}

func ItemAttentionMovieName(name string) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "AttentionMovieName", name)
		item.mark("NewAttentionIcon")
	}
}

func ItemToolTip(iconName string, iconPixmap []image.Image, title, description string) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "ToolTip", tooltip{IconName: iconName, IconPixmap: toPixmaps(iconPixmap), Title: title, Description: description})
		item.mark("NewToolTip")
	}
}

func ItemIsMenu(itemIsMenu bool) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "ItemIsMenu", itemIsMenu)
	}
}

func ItemHandler(handler Handler) ItemProp {
	return func(item *itemProps) {
		p := &handler
		if handler == nil {
			p = nil
		}
		item.handler.Store(p)
	}
}
