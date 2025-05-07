package tray

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"slices"
	"sync/atomic"

	"deedles.dev/tray/internal/set"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

const (
	itemPath   dbus.ObjectPath = "/StatusNotifierItem"
	itemInter                  = "org.freedesktop.StatusNotifierItem"
	itemInter2                 = "org.kde.StatusNotifierItem"
)

var (
	spaces     = [...]string{"freedesktop", "kde"}
	itemInters = [...]string{itemInter, itemInter2}

	itemPropsInter = map[string]*prop.Prop{
		"Category":            makeProp(ApplicationStatus),
		"Id":                  makeProp(""),
		"Title":               makeProp(""),
		"Status":              makeProp(Active),
		"WindowId":            makeProp(uint32(0)),
		"IconName":            makeProp(""),
		"IconPixmap":          makeProp[[]Pixmap](nil),
		"IconAccessibleDesc":  makeProp(""),
		"OverlayIconName":     makeProp(""),
		"OverlayIconPixmap":   makeProp[[]Pixmap](nil),
		"AttentionIconName":   makeProp(""),
		"AttentionIconPixmap": makeProp[[]Pixmap](nil),
		"AttentionMovieName":  makeProp(""),
		"ToolTip":             makeProp(tooltip{}),
		"ItemIsMenu":          makeProp(false),
		"Menu":                makeConstProp(menuPath),
	}

	itemPropsMap = prop.Map{
		itemInter:  itemPropsInter,
		itemInter2: itemPropsInter,
	}
)

// Item is a single StatusNotifierItem. Each item roughly corresponds
// to a single icon in the system tray.
type Item struct {
	conn      *dbus.Conn
	props     *prop.Properties
	menu      *Menu
	snw, name string
	handler   atomic.Pointer[Handler]
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
	item.name = getName()

	reply, err := item.conn.RequestName(item.name, 0)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		// This error is non-fatal. Using a name with a specific format is
		// essentially just a convention and is not necessary, so if it
		// fails, this just falls back to the connection's unique
		// identifier which is always available.
		logger.Warn("request name failed", "name", item.name, "reply", reply, "err", err)
		item.name = item.conn.Names()[0]
	}
	logger.Info("names acquired", "registered", item.name, "available", item.conn.Names())

	return nil
}

func (item *Item) export(props []ItemProp) error {
	err := item.conn.Export((*statusNotifierItem)(item), itemPath, itemInter)
	if err != nil {
		return fmt.Errorf("export methods as %v: %w", itemInter, err)
	}

	err = item.conn.Export((*statusNotifierItem)(item), itemPath, itemInter2)
	if err != nil {
		return fmt.Errorf("export methods as %v: %w", itemInter2, err)
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

	var errs []error
	for _, space := range spaces {
		watcher := fmt.Sprintf("org.%v.StatusNotifierWatcher", space)
		method := fmt.Sprintf("%v.RegisterStatusNotifierItem", watcher)
		err = dbusCall(item.conn.Object(watcher, "/StatusNotifierWatcher"), method, 0, item.name).Store()
		if err == nil {
			break
		}
		errs = append(errs, fmt.Errorf("register StatusNotifierItem with %v: %w", watcher, err))
	}
	if len(errs) == len(spaces) {
		return errors.Join(errs...)
	}

	return nil
}

func (item *Item) exportProps() error {
	props, err := prop.Export(item.conn, itemPath, itemPropsMap)
	if err != nil {
		return err
	}
	item.props = props
	return nil
}

func (item *Item) exportIntrospect() error {
	inter := func(name string) introspect.Interface {
		return introspect.Interface{
			Name:       name,
			Methods:    introspect.Methods((*statusNotifierItem)(item)),
			Properties: item.props.Introspection(name),
			Signals: []introspect.Signal{
				{Name: "NewTitle"},
				{Name: "NewIcon"},
				{Name: "NewAttentionIcon"},
				{Name: "NewOverlayIcon"},
				{Name: "NewToolTip"},
				{Name: "NewStatus"},
			},
		}
	}

	node := introspect.Node{
		Name: string(itemPath),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			inter(itemInter),
			inter(itemInter2),
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
	errs := make([]error, 0, len(itemInters))
	for _, inter := range itemInters {
		errs = append(errs, item.conn.Emit(itemPath, fmt.Sprintf("%v.%v", inter, name)))
	}
	return errors.Join(errs...)
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
	return item.props.GetMust(itemInter, "Category").(Category)
}

// ID returns the current value of the Id property.
func (item *Item) ID() string {
	return item.props.GetMust(itemInter, "Id").(string)
}

// Title returns the current value of the Title property.
func (item *Item) Title() string {
	return item.props.GetMust(itemInter, "Title").(string)
}

// Status returns the current value of the Status property.
func (item *Item) Status() Status {
	return item.props.GetMust(itemInter, "Status").(Status)
}

// WindowID returns the current value of the WindowId property.
func (item *Item) WindowID() uint32 {
	return item.props.GetMust(itemInter, "WindowId").(uint32)
}

// IconName returns the current value of the IconName property.
func (item *Item) IconName() string {
	return item.props.GetMust(itemInter, "IconName").(string)
}

// IconPixmap returns the current value of the IconPixmap property.
func (item *Item) IconPixmap() []image.Image {
	pixmaps := item.props.GetMust(itemInter, "IconPixmap").([]Pixmap)
	return fromPixmaps(pixmaps)
}

// IconAccessibleDesc returns the current value of the
// IconAccessibleDesc property.
func (item *Item) IconAccessibleDesc() string {
	return item.props.GetMust(itemInter, "IconAccessibleDesc").(string)
}

// OverlayIconName returns the current value of the OverlayIconName
// property.
func (item *Item) OverlayIconName() string {
	return item.props.GetMust(itemInter, "OverlayIconName").(string)
}

// OverlayIconPixmap returns the current value of the
// OverlayIconPixmap property.
func (item *Item) OverlayIconPixmap() []image.Image {
	pixmaps := item.props.GetMust(itemInter, "OverlayIconPixmap").([]Pixmap)
	return fromPixmaps(pixmaps)
}

// AttentionIconName returns the current value of the AttentionIconName
// property.
func (item *Item) AttentionIconName() string {
	return item.props.GetMust(itemInter, "AttentionIconName").(string)
}

// AttentionIconPixmap returns the current value of the
// AttentionIconPixmap property.
func (item *Item) AttentionIconPixmap() []image.Image {
	pixmaps := item.props.GetMust(itemInter, "AttentionIconPixmap").([]Pixmap)
	return fromPixmaps(pixmaps)
}

// AttentionMovieName returns the current value of the
// AttentionMovieName property.
func (item *Item) AttentionMovieName() string {
	return item.props.GetMust(itemInter, "AttentionMovieName").(string)
}

// ToolTip returns the current values of the ToolTip property.
func (item *Item) ToolTip() (iconName string, iconPixmap []image.Image, title, description string) {
	tooltip := item.props.GetMust(itemInter, "ToolTip").(tooltip)
	return tooltip.IconName, fromPixmaps(tooltip.IconPixmap), tooltip.Title, tooltip.Description
}

// IsMenu returns the current value of the ItemIsMenu property.
func (item *Item) IsMenu() bool {
	return item.props.GetMust(itemInter, "ItemIsMenu").(bool)
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

// Pixmap is the raw wire format of StatusNotifierItem icon data. The
// Data field is in a big endian [ARGB32] format. For convenience,
// Pixmap implements [draw.Image] to simplify conversion to and from
// other formats.
type Pixmap struct {
	Width, Height int
	Data          []byte
}

// ToPixmap converts an [image.Image] into a Pixmap. If an icon is
// being set to the same image more than once, it is more efficient to
// convert it to a Pixmap in advance using this function and then pass
// the result intead of the original image.Image.
func ToPixmap(img image.Image) Pixmap {
	switch p := img.(type) {
	case Pixmap:
		return p.Copy()
	case *Pixmap:
		return p.Copy()
	}

	bounds := img.Bounds()
	dst := Pixmap{
		Width:  bounds.Dx(),
		Height: bounds.Dy(),
		Data:   make([]byte, 4*bounds.Dx()*bounds.Dy()),
	}
	draw.Draw(&dst, bounds, img, image.Point{}, draw.Src)
	return dst
}

func (p Pixmap) slice(x, y int) []byte {
	i := 4 * ((y * p.Width) + x)
	return p.Data[i : i+4]
}

func (p Pixmap) ColorModel() color.Model {
	return ARGB32Model
}

func (p Pixmap) Bounds() image.Rectangle {
	return image.Rect(0, 0, p.Width, p.Height)
}

func (p Pixmap) At(x, y int) color.Color {
	s := p.slice(x, y)
	return ARGB32(binary.BigEndian.Uint32(s))
}

func (p *Pixmap) Set(x, y int, c color.Color) {
	s := p.slice(x, y)
	binary.BigEndian.PutUint32(s, uint32(argb32Model(c).(ARGB32)))
}

// Copy returns a deep copy of p.
func (p Pixmap) Copy() Pixmap {
	p.Data = slices.Clone(p.Data)
	return p
}

// ARGB32 is the color.Color implementation used by [Pixmap].
type ARGB32 uint32

func (c ARGB32) RGBA() (r, g, b, a uint32) {
	n := uint32(c)
	a = (n >> 24 * 0xFFFF / 0xFF)
	r = (n >> 16 & 0xFF) * a / 0xFF
	g = (n >> 8 & 0xFF) * a / 0xFF
	b = (n & 0xFF) * a / 0xFF
	return
}

// ARGB32Model is the color.Model implementation used by [Pixmap].
var ARGB32Model color.Model = color.ModelFunc(argb32Model)

func argb32Model(c color.Color) color.Color {
	if c, ok := c.(ARGB32); ok {
		return c
	}

	r, g, b, a := c.RGBA()
	if a == 0 {
		return ARGB32(0)
	}

	r = (r * 0xFF / a) << 16
	g = (g * 0xFF / a) << 8
	b = b * 0xFF / a
	a = (a * 0xFF / 0xFFFF) << 24
	return ARGB32(r | g | b | a)
}

func fromPixmaps(pixmaps []Pixmap) []image.Image {
	images := make([]image.Image, 0, len(pixmaps))
	for _, p := range pixmaps {
		images = append(images, p)
	}
	return images
}

func toPixmaps(images []image.Image) []Pixmap {
	pixmaps := make([]Pixmap, 0, len(images))
	for _, img := range images {
		pixmaps = append(pixmaps, ToPixmap(img))
	}
	return pixmaps
}

type tooltip struct {
	IconName           string
	IconPixmap         []Pixmap
	Title, Description string
}

// ItemProp is a function that modifies the properties of an Item.
type ItemProp func(*itemProps)

type itemProps struct {
	*Item
	dirty set.Set[string]
}

func (item *itemProps) set(prop string, v any) {
	for _, inter := range itemInters {
		item.props.SetMust(inter, prop, v)
	}
}

func (item *itemProps) mark(change string) {
	item.dirty.Add(change)
}

// ItemCategory sets the Category property to the given value.
func ItemCategory(category Category) ItemProp {
	return func(item *itemProps) {
		item.set("Category", category)
	}
}

// ItemID sets the Id property to the given value.
func ItemID(id string) ItemProp {
	return func(item *itemProps) {
		item.set("Id", id)
	}
}

// ItemTitle sets the Title property to the given value.
func ItemTitle(title string) ItemProp {
	return func(item *itemProps) {
		item.set("Title", title)
		item.mark("NewTitle")
	}
}

// ItemStatus sets the Status property to the given value.
func ItemStatus(status Status) ItemProp {
	return func(item *itemProps) {
		item.set("Status", status)
		item.mark("NewStatus")
	}
}

// ItemWindowID sets the WindowId property to the given value.
func ItemWindowID(id uint32) ItemProp {
	return func(item *itemProps) {
		item.set("WindowId", id)
	}
}

// ItemIconName sets the IconName property to the given value.
func ItemIconName(name string) ItemProp {
	return func(item *itemProps) {
		item.set("IconName", name)
		item.mark("NewIcon")
	}
}

// ItemIconPixmap sets the IconPixmap property to the given value.
func ItemIconPixmap(images ...image.Image) ItemProp {
	return func(item *itemProps) {
		item.set("IconPixmap", toPixmaps(images))
		item.mark("NewIcon")
	}
}

// ItemIconAccessibleDesc sets the IconAccessibleDesc property to the
// given value.
func ItemIconAccessibleDesc(desc string) ItemProp {
	return func(item *itemProps) {
		item.set("IconAccessibleDesc", desc)
		item.mark("NewIcon")
	}
}

// ItemOverlayIconName sets the OverlayIconName property to the given
// value.
func ItemOverlayIconName(name string) ItemProp {
	return func(item *itemProps) {
		item.set("OverlayIconName", name)
		item.mark("NewOverlayIcon")
	}
}

// ItemOverlayIconPixmap sets the OverlayIconPixmap property to the
// given value.
func ItemOverlayIconPixmap(images ...image.Image) ItemProp {
	return func(item *itemProps) {
		item.set("OverlayIconPixmap", toPixmaps(images))
		item.mark("NewOverlayIcon")
	}
}

// ItemAttentionIconName sets the AttentionIconName property to the
// given value.
func ItemAttentionIconName(name string) ItemProp {
	return func(item *itemProps) {
		item.set("AttentionIconName", name)
		item.mark("NewAttentionIcon")
	}
}

// ItemAttentionIconPixmap sets the AttentionIconPixmap property to
// the given value.
func ItemAttentionIconPixmap(images ...image.Image) ItemProp {
	return func(item *itemProps) {
		item.set("AttentionIconPixmap", toPixmaps(images))
		item.mark("NewAttentionIcon")
	}
}

// ItemAttentionMovieName sets the AttentionMovieName property to the
// given value.
func ItemAttentionMovieName(name string) ItemProp {
	return func(item *itemProps) {
		item.set("AttentionMovieName", name)
		item.mark("NewAttentionIcon")
	}
}

// ItemToolTip sets the ToolTip property to the given values.
func ItemToolTip(iconName string, iconPixmap []image.Image, title, description string) ItemProp {
	return func(item *itemProps) {
		item.set("ToolTip", tooltip{IconName: iconName, IconPixmap: toPixmaps(iconPixmap), Title: title, Description: description})
		item.mark("NewToolTip")
	}
}

// ItemIsMenu sets the ItemIsMenu property to the given value.
func ItemIsMenu(itemIsMenu bool) ItemProp {
	return func(item *itemProps) {
		item.set("ItemIsMenu", itemIsMenu)
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

var id uint64

func getName() string {
	id := atomic.AddUint64(&id, 1)
	return fmt.Sprintf("org.freedesktop.StatusNotifierItem-%v-%v", os.Getpid(), id)
}
