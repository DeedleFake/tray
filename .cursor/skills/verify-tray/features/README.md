# tray verification map

This directory is the maintained source for verifying the host-facing behavior of `deedles.dev/tray`. The "user" is a desktop tray host talking D-Bus to an item. Read the index before driving, then use the matching feature file as the recipe.

## Baseline preconditions

- Run `.cursor/skills/verify-tray/scripts/control-tray launch` from the repo root and wait for `ready:`.
- Wait 5 seconds after `ready:` so the example has renamed `Quit` to `Exit`.
- Run `control-tray doctor` and require exit 0 and `item_id` containing `dev.deedles.tray.examples.simple`.
- Never drive an item on the user's session bus. Every command here uses the run's private bus.
- Put proof under `.cursor/skills/verify-tray/artifacts/<feature>/`.

Baseline layout of `./examples` (from `control-tray layout`): root `0` with `1 Edit` (submenu: `2 Add` disabled, `3 Remove`), `4` separator, `5 Print`, `6 Exit`, then `7 Ticks` and `8 Check`. `Ticks` and `Check` change about once a second, so match those two by role rather than by the exact label or toggle state.

## Driving conventions

- Start every recipe from a fresh `launch` unless its preconditions say otherwise. Menu mutations are not reversible without relaunching ids.
- Address menu entries by the ids read from the latest `layout`, matched by label.
- Treat every command as literal.
- Do not click `Exit` (`event 6`) except in the recipe that tests it. It ends the example.

## Proof and skip reporting

- Capture the host action and the resulting state: `layout` or `item-props` before and after, plus a `monitor` signal log across the action.
- Confirm handler side effects with lines copied from `item.log`.
- Record the feature ID and entry point with every artifact.
- Report an unreachable path with the attempted command and the unmet precondition.
- Do not report a skipped entry point as verified through a different path.

## Feature entry contract

Each feature file starts with an H1 title and one paragraph describing the host-visible behavior. It then uses exactly four H2 sections in this order.

1. `Sub-features` lists short IDs with one line for each behavior.
2. `How to get to it (user POV)` lists every host entry point.
3. `Driving it with control-tray` starts with `Preconditions:` and uses labeled bullets that pair each host action with an exact command and observable result.
4. `Gotchas` lists traps that can waste or invalidate a verification run.

## Features

- [Registration](./registration.md) covers bus name acquisition and `RegisterStatusNotifierItem` with the watcher.
- [Item properties](./item-properties.md) covers introspection and the StatusNotifierItem properties under both interface names.
- [Menu layout](./menu-layout.md) covers `GetLayout`, `GetGroupProperties`, menu properties, and `AboutToShow`.
- [Menu events](./menu-events.md) covers clicking entries through `Event`, the resulting layout changes, and `LayoutUpdated`/`ItemsPropertiesUpdated` signals.
- [Item activation](./item-activation.md) covers `Activate`, `SecondaryActivate`, `ContextMenu`, and `Scroll` on the icon.
