package tray

import (
	"sync"

	"deedles.dev/tray/internal/set"
	"github.com/godbus/dbus/v5"
)

// pendingReplies tracks the dbusmenu calls whose replies carry item
// properties, from the moment a call arrives until godbus has written
// its reply. godbus writes a reply after the method returns and
// Menu.m is unlocked, so a property change in that window sends its
// ItemsPropertiesUpdated signal ahead of a reply that still holds the
// old value, and the host applies the old value last. The tracker
// records which items changed in that window so that their properties
// can be sent again once the reply is out.
type pendingReplies struct {
	m       sync.Mutex
	calls   map[pendingCall]set.Set[int]
	replies map[uint32]set.Set[int]
}

type pendingCall struct {
	sender string
	serial uint32
}

// received is an incoming interceptor. godbus runs it before it
// starts the method, so before the method reads any property.
func (p *pendingReplies) received(msg *dbus.Message) {
	if msg.Type != dbus.TypeMethodCall || msg.Flags&dbus.FlagNoReplyExpected != 0 {
		return
	}
	path, _ := msg.Headers[dbus.FieldPath].Value().(dbus.ObjectPath)
	member, _ := msg.Headers[dbus.FieldMember].Value().(string)
	if path != menuPath {
		return
	}
	switch member {
	case "GetLayout", "GetGroupProperties", "GetProperty":
	default:
		return
	}
	sender, _ := msg.Headers[dbus.FieldSender].Value().(string)

	p.m.Lock()
	defer p.m.Unlock()

	if p.calls == nil {
		p.calls = make(map[pendingCall]set.Set[int])
	}
	p.calls[pendingCall{sender: sender, serial: msg.Serial()}] = make(set.Set[int])
}

// sending is an outgoing interceptor. godbus runs it after it assigns
// the message a serial and before it writes the message.
func (p *pendingReplies) sending(msg *dbus.Message) {
	if msg.Type != dbus.TypeMethodReply && msg.Type != dbus.TypeError {
		return
	}
	dest, _ := msg.Headers[dbus.FieldDestination].Value().(string)
	serial, _ := msg.Headers[dbus.FieldReplySerial].Value().(uint32)

	p.m.Lock()
	defer p.m.Unlock()

	call := pendingCall{sender: dest, serial: serial}
	changed, ok := p.calls[call]
	if !ok {
		return
	}
	delete(p.calls, call)

	if p.replies == nil {
		p.replies = make(map[uint32]set.Set[int])
	}
	p.replies[msg.Serial()] = changed
}

// changed records that the properties of the item with the given ID
// changed while the pending replies may hold older values.
func (p *pendingReplies) changed(id int) {
	p.m.Lock()
	defer p.m.Unlock()

	for _, changed := range p.calls {
		changed.Add(id)
	}
	for _, changed := range p.replies {
		changed.Add(id)
	}
}

// written stops tracking the reply with the given serial, which godbus
// has written, and returns the IDs of the items that changed after
// the reply read them.
func (p *pendingReplies) written(serial uint32) set.Set[int] {
	p.m.Lock()
	defer p.m.Unlock()

	changed := p.replies[serial]
	delete(p.replies, serial)
	return changed
}

// replyWritten resends the properties of the items that changed after
// the reply with the given serial read them, so that the host applies
// the current values after the reply's.
func (item *Item) replyWritten(serial uint32) {
	changed := item.replies.written(serial)
	if len(changed) == 0 {
		return
	}

	menu := item.menu
	menu.m.RLock()
	defer menu.m.RUnlock()

	for id := range changed {
		node := menu.nodes[id]
		if node == nil {
			continue
		}

		node.m.RLock()
		updated := make(map[string]dbus.Variant, len(node.props))
		for name, v := range node.props {
			updated[name] = dbus.MakeVariant(v)
		}
		err := node.sendPropertiesUpdated(updated)
		node.m.RUnlock()
		if err != nil {
			logger.Error("resend menu item properties", "id", id, "err", err)
		}
	}
}
