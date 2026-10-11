# Item properties

A tray host reads the icon's id, title, pixmap, tooltip, status, and menu path from the item's properties. tray serves the same properties under `org.kde.StatusNotifierItem` and `org.freedesktop.StatusNotifierItem`.

## Sub-features

- `props-introspect` advertises both item interfaces, Properties, and the `New*` signals at `/StatusNotifierItem`.
- `props-values` returns `Id`, `Title`, `ToolTip`, `ItemIsMenu`, `IconPixmap`, `Menu`, `Status`, `Category`.
- `props-both-ifaces` returns the same values under both interface names.

## How to get to it (user POV)

- The host calls `Introspect` and `Properties.GetAll` on `/StatusNotifierItem` after registration.

## Driving it with control-tray

Preconditions:

- `control-tray doctor` exits 0.

- **Introspect.** Run `control-tray introspect > artifacts/item-properties/introspect.txt`. It lists `interface org.kde.StatusNotifierItem` and `interface org.freedesktop.StatusNotifierItem`, each with `Activate`, `ContextMenu`, `SecondaryActivate`, `Scroll`, and signals `NewTitle`, `NewIcon`, `NewToolTip`, `NewStatus`.
- **Read props.** Run `control-tray item-props > artifacts/item-properties/kde.txt`. It contains `'Id': <'dev.deedles.tray.examples.simple'>`, `'Title': <'Simple Example'>`, `'ItemIsMenu': <true>`, `'Menu': <objectpath '/StatusNotifierMenu'>`, `'Status': <'Active'>`, `'ToolTip': <('', @a(iiay) [], 'Simple Example', 'A simple example of a tray icon.')>`, and `'IconPixmap': <[(32, 32, [byte ...`.
- **Other interface.** Run `control-tray item-props org.freedesktop.StatusNotifierItem > artifacts/item-properties/freedesktop.txt`. Same values.
- **One prop.** Run `control-tray prop Title`. Reply is `(<'Simple Example'>,)`.

## Gotchas

- `IconPixmap` is several kilobytes of bytes. grep for `(32, 32,` rather than reading it.
- The example does not change item properties after start, so `New*` signals are not observable with it.
