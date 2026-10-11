# Menu layout

A tray host reads the menu tree and per-entry properties from `com.canonical.dbusmenu` at `/StatusNotifierMenu` before showing it, and asks whether a submenu needs refreshing before opening it.

## Sub-features

- `layout-full` returns the whole tree from root `0` with depth `-1`.
- `layout-sub` returns one submenu by parent id and depth.
- `layout-group` returns properties for all or selected ids.
- `layout-menu-props` returns `Version` 3, `TextDirection`, `Status`, `IconThemePath`.
- `layout-about-to-show` answers `AboutToShow` for a submenu.

## How to get to it (user POV)

- The host calls `GetLayout(0, -1, [])` when the icon is clicked or after `LayoutUpdated`.
- It calls `GetGroupProperties` and `AboutToShow` when opening a submenu.

## Driving it with control-tray

Preconditions:

- `control-tray doctor` exits 0, five seconds after `launch`.

- **Full tree.** Run `control-tray layout > artifacts/menu-layout/layout.txt`. Root children are `1 Edit` with `'children-display': <'submenu'>` containing `2 Add` (`'enabled': <false>`) and `3 Remove`, then `4` `'type': <'separator'>`, `5 Print`, `6 Exit`.
- **Submenu.** Run `control-tray layout 1 1`. The reply root is id `1` with children `2` and `3` only.
- **Group props.** Run `control-tray group-props > artifacts/menu-layout/group.txt`. It lists ids 1 through 6 with the labels above. Run `control-tray group-props 2,6`. Only ids 2 and 6 come back.
- **Menu props.** Run `control-tray menu-props`. Reply is `{'Status': <'normal'>, 'IconThemePath': <@as []>, 'Version': <3>, 'TextDirection': <'ltr'>}` in any order.
- **About to show.** Run `control-tray about-to-show 1`. Reply is `(false,)` right after a `layout` read.
- **Proof.** Also save `control-tray introspect /StatusNotifierMenu` to `artifacts/menu-layout/introspect.txt`.

## Gotchas

- `group-props` with no ids returns entries in map order, not id order.
- `GetLayout` clears the dirty flag `AboutToShow` reports, so `about-to-show` after `layout` is always `false`. To see `true`, mutate the menu (see menu-events) and call `about-to-show` on the parent before any `layout`.
- `layout` depth is passed as `int32`; a bare `-1` would be parsed by `gdbus` as a flag. Use the helper.
