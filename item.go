package tray

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"slices"
	"sync/atomic"

	"deedles.dev/tray/internal/set"
	"deedles.dev/ximage/format"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

const itemPath dbus.ObjectPath = "/StatusNotifierItem"

// Item is a single StatusNotifierItem. Each item roughly corresponds
// to a single icon in the system tray.
type Item struct {
	conn               *dbus.Conn
	props              *prop.Properties
	menu               *Menu
	space, inter, name string
	handler            atomic.Pointer[Handler]
}

// New creates a new Item configured with the given props. It is
// recommended to at least set the ID and Icon properties, though
// these can be set later if preferred. The icon's behavior without
// them set will likely not be useful, however, if it isn't completely
// broken depending on the desktop environment.
func New(props ...ItemProp) (*Item, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("connect to session bus: %w", err)
	}

	item := Item{
		conn: conn,
	}

	err = item.initProtoData()
	if err != nil {
		return nil, fmt.Errorf("initialize protocol data: %w", err)
	}

	err = item.export(props)
	if err != nil {
		return nil, fmt.Errorf("export StatusNotifierItem: %w", err)
	}

	return &item, nil
}

func (item *Item) initProtoData() error {
	space, err := getSpace(item.conn)
	if err != nil {
		return fmt.Errorf("get namespace: %w", err)
	}
	item.space = space
	item.inter = fmt.Sprintf("org.%v.StatusNotifierItem", space)
	item.name = getName(space)

	reply, err := item.conn.RequestName(item.name, 0)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		// This error is non-fatal. Using a name with a specific format is
		// essentially just a convention and is not necessary, so if it
		// fails, this just falls back to the connection's unique
		// identifier which is always available.
		logger.Warn("request name failed", "name", item.name, "reply", reply, "err", err)
		item.name = item.conn.Names()[0]
	}
	logger.Info("name chosen", "name", item.name)

	return nil
}

func (item *Item) export(props []ItemProp) error {
	err := item.conn.Export((*statusNotifierItem)(item), itemPath, item.inter)
	if err != nil {
		return fmt.Errorf("export methods: %w", err)
	}

	err = item.exportProps()
	if err != nil {
		return fmt.Errorf("export properties: %w", err)
	}

	err = item.exportIntrospect()
	if err != nil {
		return fmt.Errorf("export introspect data: %w", err)
	}

	err = item.createMenu()
	if err != nil {
		return fmt.Errorf("create menu: %w", err)
	}

	err = item.SetProps(props...)
	if err != nil {
		return fmt.Errorf("set properties: %w", err)
	}

	watcher := fmt.Sprintf("org.%v.StatusNotifierWatcher", item.space)
	method := fmt.Sprintf("%v.RegisterStatusNotifierItem", watcher)
	err = item.conn.Object(watcher, "/StatusNotifierWatcher").Call(method, 0, item.name).Store()
	if err != nil {
		return fmt.Errorf("register StatusNotifierItem with %v: %w", watcher, err)
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

// Close closes the underlying D-Bus connection, removing the item
// from the tray. The behavior of any calls to any methods either on
// this Item or on any associated Menu or MenuItem instances after
// calling this is undefined.
func (item *Item) Close() error {
	return item.conn.Close()
}

func (item *Item) emit(name string) error {
	return item.conn.Emit(itemPath, fmt.Sprintf("org.%v.StatusNotifierItem.%v", item.space, name))
}

// SetProps sets the given properties for the item, emits any
// necessary signals after setting all of them, and then returns any
// errors that happened. A non-nil error return does not necessarily
// indicate complete failure.
func (item *Item) SetProps(props ...ItemProp) error {
	w := itemProps{Item: item, dirty: make(set.Set[string])}
	for _, p := range props {
		p(&w)
	}

	errs := make([]error, 0, len(w.dirty))
	for s := range w.dirty {
		errs = append(errs, item.emit(s))
	}

	return errors.Join(errs...)
}

// Category returns the current value of the Category property.
func (item *Item) Category() Category {
	return item.props.GetMust(item.inter, "Category").(Category)
}

// ID returns the current value of the Id property.
func (item *Item) ID() string {
	return item.props.GetMust(item.inter, "Id").(string)
}

// Title returns the current value of the Title property.
func (item *Item) Title() string {
	return item.props.GetMust(item.inter, "Title").(string)
}

// Status returns the current value of the Status property.
func (item *Item) Status() Status {
	return item.props.GetMust(item.inter, "Status").(Status)
}

// WindowID returns the current value of the WindowId property.
func (item *Item) WindowID() uint32 {
	return item.props.GetMust(item.inter, "WindowId").(uint32)
}

// IconName returns the current value of the IconName property.
func (item *Item) IconName() string {
	return item.props.GetMust(item.inter, "IconName").(string)
}

// IconPixmap returns the current value of the IconPixmap property.
func (item *Item) IconPixmap() []image.Image {
	pixmaps := item.props.GetMust(item.inter, "IconPixmap").([]pixmap)
	return fromPixmaps(pixmaps)
}

// IconAccessibleDesc returns the current value of the
// IconAccessibleDesc property.
func (item *Item) IconAccessibleDesc() string {
	return item.props.GetMust(item.inter, "IconAccessibleDesc").(string)
}

// OverlayIconName returns the current value of the OverlayIconName
// property.
func (item *Item) OverlayIconName() string {
	return item.props.GetMust(item.inter, "OverlayIconName").(string)
}

// OverlayIconPixmap returns the current value of the
// OverlayIconPixmap property.
func (item *Item) OverlayIconPixmap() []image.Image {
	pixmaps := item.props.GetMust(item.inter, "OverlayIconPixmap").([]pixmap)
	return fromPixmaps(pixmaps)
}

// AttentionIconName returns the current value of the AttentionIconName
// property.
func (item *Item) AttentionIconName() string {
	return item.props.GetMust(item.inter, "AttentionIconName").(string)
}

// AttentionIconPixmap returns the current value of the
// AttentionIconPixmap property.
func (item *Item) AttentionIconPixmap() []image.Image {
	pixmaps := item.props.GetMust(item.inter, "AttentionIconPixmap").([]pixmap)
	return fromPixmaps(pixmaps)
}

// AttentionMovieName returns the current value of the
// AttentionMovieName property.
func (item *Item) AttentionMovieName() string {
	return item.props.GetMust(item.inter, "AttentionMovieName").(string)
}

// ToolTip returns the current values of the ToolTip property.
func (item *Item) ToolTip() (iconName string, iconPixmap []image.Image, title, description string) {
	tooltip := item.props.GetMust(item.inter, "ToolTip").(tooltip)
	return tooltip.IconName, fromPixmaps(tooltip.IconPixmap), tooltip.Title, tooltip.Description
}

// IsMenu returns the current value of the ItemIsMenu property.
func (item *Item) IsMenu() bool {
	return item.props.GetMust(item.inter, "ItemIsMenu").(bool)
}

// Menu returns the Menu instance associated with the Item.
func (item *Item) Menu() *Menu {
	return item.menu
}

// Handler returns the Handler that is used to handle events triggered
// by the desktop environment.
func (item *Item) Handler() Handler {
	h := item.handler.Load()
	if h == nil {
		return nil
	}
	return *h
}

// Category is the possible values of the Category Item property.
type Category string

const (
	ApplicationStatus Category = "ApplicationStatus"
	Communications    Category = "Communications"
	SystemServices    Category = "SystemServices"
	Hardware          Category = "Hardware"
)

// Status is the possible values of the Status Item property.
type Status string

const (
	Passive        Status = "Passive"
	Active         Status = "Active"
	NeedsAttention Status = "NeedsAttention"
)

// Orientation is the possible directions of a scroll event.
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

// ItemProp is a function that modifies the properties of an Item.
type ItemProp func(*itemProps)

type itemProps struct {
	*Item
	dirty set.Set[string]
}

func (item *itemProps) mark(change string) {
	item.dirty.Add(change)
}

// ItemCategory sets the Category property to the given value.
func ItemCategory(category Category) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "Category", category)
	}
}

// ItemID sets the Id property to the given value.
func ItemID(id string) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "Id", id)
	}
}

// ItemTitle sets the Title property to the given value.
func ItemTitle(title string) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "Title", title)
		item.mark("NewTitle")
	}
}

// ItemStatus sets the Status property to the given value.
func ItemStatus(status Status) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "Status", status)
		item.mark("NewStatus")
	}
}

// ItemWindowID sets the WindowId property to the given value.
func ItemWindowID(id uint32) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "WindowId", id)
	}
}

// ItemIconName sets the IconName property to the given value.
func ItemIconName(name string) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "IconName", name)
		item.mark("NewIcon")
	}
}

// ItemIconPixmap sets the IconPixmap property to the given value.
func ItemIconPixmap(images ...image.Image) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "IconPixmap", toPixmaps(images))
		item.mark("NewIcon")
	}
}

// ItemIconAccessibleDesc sets the IconAccessibleDesc property to the
// given value.
func ItemIconAccessibleDesc(desc string) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "IconAccessibleDesc", desc)
		item.mark("NewIcon")
	}
}

// ItemOverlayIconName sets the OverlayIconName property to the given
// value.
func ItemOverlayIconName(name string) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "OverlayIconName", name)
		item.mark("NewOverlayIcon")
	}
}

// ItemOverlayIconPixmap sets the OverlayIconPixmap property to the
// given value.
func ItemOverlayIconPixmap(images ...image.Image) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "OverlayIconPixmap", toPixmaps(images))
		item.mark("NewOverlayIcon")
	}
}

// ItemAttentionIconName sets the AttentionIconName property to the
// given value.
func ItemAttentionIconName(name string) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "AttentionIconName", name)
		item.mark("NewAttentionIcon")
	}
}

// ItemAttentionIconPixmap sets the AttentionIconPixmap property to
// the given value.
func ItemAttentionIconPixmap(images ...image.Image) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "AttentionIconPixmap", toPixmaps(images))
		item.mark("NewAttentionIcon")
	}
}

// ItemAttentionMovieName sets the AttentionMovieName property to the
// given value.
func ItemAttentionMovieName(name string) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "AttentionMovieName", name)
		item.mark("NewAttentionIcon")
	}
}

// ItemToolTip sets the ToolTip property to the given values.
func ItemToolTip(iconName string, iconPixmap []image.Image, title, description string) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "ToolTip", tooltip{IconName: iconName, IconPixmap: toPixmaps(iconPixmap), Title: title, Description: description})
		item.mark("NewToolTip")
	}
}

// ItemIsMenu sets the ItemIsMenu property to the given value.
func ItemIsMenu(itemIsMenu bool) ItemProp {
	return func(item *itemProps) {
		item.props.SetMust(item.inter, "ItemIsMenu", itemIsMenu)
	}
}

// ItemHandler sets the Item's handler.
func ItemHandler(handler Handler) ItemProp {
	return func(item *itemProps) {
		p := &handler
		if handler == nil {
			p = nil
		}
		item.handler.Store(p)
	}
}
