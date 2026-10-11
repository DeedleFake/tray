---
name: verify-tray
description: Drive a deedles.dev/tray StatusNotifierItem and its dbusmenu over a private D-Bus session bus, the way a desktop tray host does. Use when proving a change to tray's item properties, menu layout, menu events, item activation, or watcher registration against the live examples program.
---

# Verify tray

`deedles.dev/tray` is a library. Its user is a desktop tray host (Plasma, the GNOME AppIndicator extension, waybar, and so on). The host finds an item through `org.kde.StatusNotifierWatcher`, reads `org.kde.StatusNotifierItem` properties at `/StatusNotifierItem`, and reads and drives the menu through `com.canonical.dbusmenu` at `/StatusNotifierMenu`. This skill plays the host's part with `gdbus` against the `./examples` program from this checkout.

Set `CTRL` to the helper and run every command from the repo root:

```bash
CTRL=.cursor/skills/verify-tray/scripts/control-tray
```

Read `features/README.md` before a proof. Drive every entry point the matching feature file lists.

## Isolation

`launch` starts its own `dbus-daemon --session` on a socket under `runs/<id>/bus`. Every helper command talks only to that bus. The user's session bus, tray host, and tray apps are never touched.

`tray.New` fails unless a StatusNotifierWatcher answers `RegisterStatusNotifierItem`. On the private bus that is `scripts/watcher`, a stand-in that owns `org.kde.StatusNotifierWatcher` and `org.freedesktop.StatusNotifierWatcher`, logs each registration, and publishes `RegisteredStatusNotifierItems`. It is verification scaffolding, not part of the package. It does not unregister items that vanish.

Only one verify run at a time per checkout. `launch` refuses while `runs/current` points to a live bus.

## Launch

Needs `go` (the version in `go.mod`), `dbus-daemon`, `gdbus` (GLib), `setsid`, and `stdbuf`. No display is needed.

```bash
$CTRL launch                       # builds ./examples
$CTRL launch --example ./path/pkg  # any main package in this module that calls tray.New
```

`launch` builds the example and the watcher into `runs/<id>/`, starts the bus, the watcher, then the example with `TRAY_DEBUG=1` and `LANG`/`LC_ALL=C.UTF-8`, `LANGUAGE=en`. It is ready when it prints `ready: run=...` (the watcher logged `RegisterStatusNotifierItem`). It also prints an `export DBUS_SESSION_BUS_ADDRESS=...` line for running raw `gdbus` or `dbus-monitor` against the same bus. Default `--timeout` is 120 seconds.

If the build fails, `launch` removes the run directory and exits nonzero. If the item never registers, it keeps `runs/<id>/` and prints the tail of `item.log`; run `cleanup --keep-run` to stop processes and read the logs.

The example changes the `Quit` label to `Exit` five seconds after start. Recipes assume that already happened; wait 5 seconds after `launch` before taking a baseline.

## Doctor

```bash
$CTRL doctor
```

Prints JSON and exits nonzero unless all of these hold:

- `bus_alive`, `watcher_alive`, `item_process_alive` true
- `registered_item` is `org.freedesktop.StatusNotifierItem-<pid>-1`
- `item_name_owned` true
- `item_id` contains `dev.deedles.tray.examples.simple` for the default example

Run doctor before the first drive, after any failed drive, and after anything that might end the example (clicking `Exit`). After `Exit`, `item_process_alive` and `item_name_owned` are false while `registered_item` keeps the stale name; that is the stand-in, not the library.

## Drive

Every command resolves the item's bus name through the watcher and prints the raw `gdbus` reply.

| Host action | Command | D-Bus call |
| --- | --- | --- |
| Read object tree | `$CTRL introspect [/StatusNotifierItem\|/StatusNotifierMenu]` | `Introspect` |
| Read item properties | `$CTRL item-props [org.kde.StatusNotifierItem\|org.freedesktop.StatusNotifierItem]` | `Properties.GetAll` |
| Read one item property | `$CTRL prop <Name>` | `Properties.Get` |
| Read menu properties | `$CTRL menu-props` | `Properties.GetAll com.canonical.dbusmenu` |
| Read menu tree | `$CTRL layout [parentID] [depth]` (default `0 -1`) | `GetLayout` |
| Read item props in bulk | `$CTRL group-props [1,2,3]` (empty = all) | `GetGroupProperties` |
| Open a submenu | `$CTRL about-to-show <id>` | `AboutToShow` |
| Click a menu entry | `$CTRL event <id> [clicked\|hovered\|opened\|closed]` | `Event` |
| Left-click icon | `$CTRL activate <x> <y>` | `Activate` |
| Middle-click icon | `$CTRL secondary-activate <x> <y>` | `SecondaryActivate` |
| Right-click icon | `$CTRL context-menu <x> <y>` | `ContextMenu` |
| Scroll on icon | `$CTRL scroll <delta> <vertical\|horizontal>` | `Scroll` |
| Record signals | `$CTRL monitor --path <file>` ... `$CTRL monitor-stop` | `gdbus monitor` on the item's name |
| Anything else | `$CTRL bus -- <cmd>` (runs `<cmd>` with the private bus address) | |

`$CTRL logs` prints the paths of `item.log` (the example's stdout plus `TRAY_DEBUG` logs of every method the item served) and `watcher.log`.

Stable handles in `./examples`: menu ids `1` `Edit` (submenu), `2` `Add`, `3` `Remove`, `4` separator, `5` `Print`, `6` `Exit`. `Add` then `Remove` recreates `Print` with a new id (`7`, `8`, ...). Read ids from `layout` instead of assuming them after any mutation. Match on labels.

## Evidence

Store proof under `.cursor/skills/verify-tray/artifacts/<feature>/`. `cleanup` deletes `runs/` and never deletes `artifacts/`. Both are gitignored.

Proof standards:

1. Drive the host path: a D-Bus method call from outside the process, never an internal setter or a test.
2. For anything that changes state, capture `layout` or `item-props` before and after, and record signals with `monitor` across the action.
3. Confirm side effects in the example's stdout (`Print clicked.`, `Activated.`) from `item.log`. Copy the relevant lines into the artifact directory before `cleanup`, since `runs/` is deleted.
4. No mocks besides the watcher stand-in, which replaces only the desktop's watcher at the D-Bus boundary.

## Cleanup

```bash
$CTRL cleanup            # stop and delete runs/<id>
$CTRL cleanup --keep-run # stop, keep runs/<id> for logs
```

Kills only the process groups recorded in `runs/<id>/state` (bus, watcher, example, monitor). It never kills by name. Artifacts stay.

## Helpers

- `scripts/control-tray` (bash, executable): every command above.
- `scripts/watcher/main.go`: the StatusNotifierWatcher stand-in. `launch` builds it; you never run it directly. Go skips dot-directories in `./...`, so it is not linted or tested by CI.
