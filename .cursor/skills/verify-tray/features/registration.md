# Registration

A tray item takes a well-known name `org.freedesktop.StatusNotifierItem-<pid>-<n>` on the session bus and registers it with the StatusNotifierWatcher, so a tray host can find it.

## Sub-features

- `reg-name` owns `org.freedesktop.StatusNotifierItem-<pid>-1`.
- `reg-watcher` calls `RegisterStatusNotifierItem` with that name.
- `reg-restart` calls `RegisterStatusNotifierItem` again when a StatusNotifierWatcher name gains a new owner.
- `reg-gone` releases the name when the program exits.

## How to get to it (user POV)

- Start a program that calls `tray.New` while a watcher is on the bus.
- The host reads `RegisteredStatusNotifierItems` from the watcher.
- If the watcher process exits and a new one claims the well-known names, the item registers again.

## Driving it with control-tray

Preconditions:

- No live run (`control-tray cleanup` first if `runs/current` exists).

- **Launch.** Run `control-tray launch`. It prints `ready: run=<dir>`.
- **Watcher saw it.** Run `control-tray bus -- gdbus call -e -d org.kde.StatusNotifierWatcher -o /StatusNotifierWatcher -m org.freedesktop.DBus.Properties.Get org.kde.StatusNotifierWatcher RegisteredStatusNotifierItems`. Reply lists `org.freedesktop.StatusNotifierItem-<pid>-1/StatusNotifierItem`.
- **Name owned.** Run `control-tray doctor`. `registered_item` names the same service and `item_name_owned` is true.
- **Restart.** Run `control-tray restart-watcher`. It prints `restarted: watcher pid=...`.
- **Re-registered.** Run `control-tray doctor`. It exits 0 with the same `registered_item` service and `item_name_owned` true. `watcher.log` (from `control-tray logs`) has a new `RegisterStatusNotifierItem` line for that service after the restart.
- **Exit releases.** Run `control-tray event 6 clicked`, wait one second, then `control-tray doctor`. `item_process_alive` and `item_name_owned` are false and doctor exits 1.
- **Proof.** Before `cleanup`, copy `runs/current/watcher.log` and the doctor outputs to `.cursor/skills/verify-tray/artifacts/registration/`.

## Gotchas

- Without a watcher `tray.New` returns an error and the example panics. `item.log` then shows `panic: export StatusNotifierItem: register StatusNotifierItem with ...` for both watcher names.
- The stand-in never removes entries, so `registered_item` stays after exit. Use `item_name_owned`.
- The stand-in claims both well-known names one after the other. After `restart-watcher`, the item may register twice and `RegisteredStatusNotifierItems` may list the same service twice.
