# Menu events

Clicking a menu entry in the tray sends `Event(id, "clicked", data, timestamp)`. The example's handlers print, add and remove a `Print` entry, toggle `Add`/`Remove` enabled state, and exit. Adding or removing an entry emits `LayoutUpdated`. Changing `enabled`, a label, or a toggle state emits `ItemsPropertiesUpdated` and does not emit `LayoutUpdated`.

## Sub-features

- `events-click` runs the entry's click handler (`Print` prints `Print clicked.`).
- `events-remove` removes an entry and emits `LayoutUpdated` for its parent.
- `events-add` adds an entry with a new id at the requested position.
- `events-props` changes `enabled` and emits `ItemsPropertiesUpdated`.
- `events-exit` runs `Exit`, ending the program.

## How to get to it (user POV)

- The host sends `Event` when the user clicks an entry in the open tray menu.

## Driving it with control-tray

Preconditions:

- Fresh `launch`, five seconds elapsed, `control-tray doctor` exits 0, layout equals the baseline.

- **Record.** Run `control-tray monitor --path .cursor/skills/verify-tray/artifacts/menu-events/signals.txt` and `control-tray layout > .cursor/skills/verify-tray/artifacts/menu-events/before.layout.txt`.
- **Print.** Run `control-tray event 5 clicked`. Reply `()`. `item.log` (path from `control-tray logs`) gains `Print clicked.`.
- **Remove.** Run `control-tray event 3 clicked`, then `control-tray layout > .cursor/skills/verify-tray/artifacts/menu-events/after-remove.layout.txt`. Id `5` is gone, `2 Add` is `'enabled': <true>`, `3 Remove` is `'enabled': <false>`, the revision went up.
- **Add.** Run `control-tray event 2 clicked`, then `control-tray layout > .cursor/skills/verify-tray/artifacts/menu-events/after-add.layout.txt`. A new `Print` (id `9`) sits between `4` and `6`. `Add` is disabled and `Remove` enabled again.
- **Signals.** Run `control-tray monitor-stop`. `signals.txt` has `com.canonical.dbusmenu.LayoutUpdated (uint32 <rev>, 0)` for the add and the remove, and `com.canonical.dbusmenu.ItemsPropertiesUpdated` lines for ids 2, 3, and 9. It does not have `LayoutUpdated` for parent `1`. `Ticks` and `Check` also emit `ItemsPropertiesUpdated` about once a second, without `LayoutUpdated`.
- **Exit.** Last: run `control-tray event 6 clicked`, wait one second, run `control-tray doctor`. `item_process_alive` is false.
- **Proof.** Before `cleanup`, `grep 'Print clicked\|menu method' <item.log> > .cursor/skills/verify-tray/artifacts/menu-events/item-log.txt`.

## Gotchas

- `ItemsPropertiesUpdated` is emitted as `a(ia{sv})`, matching the dbusmenu spec and the menu's introspection. `gdbus monitor` shows `[(2, {'enabled': <true>})]`.
- Ids are not reused. After Remove/Add, `Print` is `9`, then `10`.
- `Add` while disabled still runs the handler: tray does not filter `Event` on `enabled`. The example's handler would add a second `Print`. Do not click disabled entries unless testing that.
- The `Exit` reply `()` arrives before the process exits. Check doctor, not the reply.
