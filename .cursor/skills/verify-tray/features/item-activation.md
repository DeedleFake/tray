# Item activation

Clicking, middle-clicking, right-clicking, or scrolling on the tray icon makes the host call `Activate`, `SecondaryActivate`, `ContextMenu`, or `Scroll` on the item, which forwards to the program's `Handler`.

## Sub-features

- `activate-primary` runs `Handler.Activate` (the example prints `Activated.`).
- `activate-secondary` returns success with the example's `ActivateHandler` no-op.
- `activate-context` returns success with the no-op.
- `activate-scroll` returns success with the no-op.

## How to get to it (user POV)

- Left click, middle click, right click, or scroll wheel on the icon in the tray.

## Driving it with control-tray

Preconditions:

- `control-tray doctor` exits 0.

- **Left click.** Run `control-tray activate 10 20`. Reply `()`. `item.log` gains `Activated.` and `item method ... name=Activate x=10 y=20`.
- **Middle click.** Run `control-tray secondary-activate 1 2`. Reply `()`. `item.log` gains `name=SecondaryActivate`.
- **Right click.** Run `control-tray context-menu 1 2`. Reply `()`. `item.log` gains `name=ContextMenu`.
- **Scroll.** Run `control-tray scroll 3 vertical`. Reply `()`. `item.log` gains `name=Scroll delta=3 orientation=vertical`.
- **Proof.** `grep 'Activated\|item method' <item.log> > artifacts/item-activation/item-log.txt` before `cleanup`.

## Gotchas

- Calls go to the `org.kde.StatusNotifierItem` interface. The freedesktop name works the same through `control-tray bus -- gdbus call ...`.
- `ItemIsMenu` is true for the example, so a real host may show the menu instead of calling `Activate`. The method still works when called.
