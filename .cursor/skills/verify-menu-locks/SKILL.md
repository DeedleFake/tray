---
name: verify-menu-locks
description: Model-check that deedles.dev/tray's Menu.m and MenuItem.m locks can never deadlock, using a TLA+ model of Go's sync.RWMutex with app goroutines calling the Menu and MenuItem API while godbus serves GetLayout, GetGroupProperties, GetProperty, Event, AboutToShow, and AboutToShowGroup on goroutines of their own. Code shapes are read from every lock site in the package, and any lock site the model does not know stops the run. Use when a change adds, removes, moves, or reorders a Lock, RLock, Unlock, RUnlock, or lock() call, calls a locking method from code that holds a lock, changes when a MenuEventHandler or prop callback runs, or bumps godbus, or when running /verify-menu-locks.
---

# Verify menu locks

The menu has one `sync.RWMutex` on `Menu` and one on each `MenuItem`. App code takes them through the `Menu` and `MenuItem` methods, and godbus takes them on a new goroutine for every method call the tray host sends. The model in `model/MenuLocks.tla` runs those goroutines one lock step at a time. TLC, the TLA+ model checker, tries every order of those steps within the bounds below and looks for a goroutine that can never move again. The deadlock fixed in #3 was found by a test that happened to hit the right timing. This check finds that shape without needing the timing.

The model follows Go's `RWMutex` exactly where it matters. Once `Lock` is waiting, new `RLock` calls block until that writer is done, so a goroutine that read-locks a mutex it already reads can wait on itself.

## Run it

From the repo root:

```bash
.cursor/skills/verify-menu-locks/scripts/check-menu-locks
```

It needs Java 11 or newer, `perl`, and `curl`. Without a system Java, `mise exec java@temurin-21 -- <command>` works. The first run downloads `tla2tools.jar` 1.8.0 to `~/.cache/deedles-tray/` and checks its sha256. `verify-menu-sync` uses the same copy. Set `TLA2TOOLS_JAR` to use another copy. A run takes about 6 seconds. `TLC_WORKERS=2` limits the cores it uses. CI runs it in the `formal verification` job of `.github/workflows/test.yml`.

To check one property alone:

```bash
.cursor/skills/verify-menu-locks/scripts/check-menu-locks --only NoDeadlock
```

Output goes to `.cursor/skills/verify-menu-locks/out/`, which git ignores.

## What it checks

Each property is an invariant over every reachable state.

| Property | Plain meaning | Harm when broken |
|---|---|---|
| `NoDeadlock` | No group of goroutines waits on each other forever. | The app's call into the menu never returns, which freezes its main thread, and the tray stops answering the host, so the menu no longer opens. |
| `MenuLockedFirst` | No goroutine asks for `Menu.m` while it holds or is waiting for an item's lock. | This is the lock order that #3 restored. Breaking it lets two goroutines each hold the lock the other wants. It fails even when the bounds are too small to show the full deadlock. |
| `NoRecursiveRead` | No goroutine read-locks a mutex it already holds. | A second `RLock` blocks behind any waiting writer, and that writer waits for the first `RLock`, so the goroutine deadlocks as soon as anything writes. |
| `HandlerRunsUnlocked` | An app's `MenuEventHandler` runs with no menu lock held. | A click handler that changes the menu, for example with `SetProps`, waits on a lock its own goroutine holds. |

`TypeOK` and `NoLockLeft` always run with them. `NoLockLeft` says a goroutine that finished its call holds no lock.

## Bounds and assumptions

The script sets the bounds. Raise one with an environment variable when a change needs a deeper search.

- `INIT_ITEMS=2`: the menu starts with 2 items, in every tree shape, so an item can be under the root or under the other item.
- `MAX_ITEMS=3`: one more item can be added with `AddChild`.
- `MAX_OPS=2`: at most 2 app calls in total.
- `MAX_CALLS=2`: at most 2 host method calls in total.
- `APP_GOROUTINES=2`: app calls come from up to 2 goroutines at once.
- `CALL_GOROUTINES=1`: one host call runs at a time. An `Event` handler still runs app code on that goroutine, so app code and host calls overlap. Deeper runs also pass. `CALL_GOROUTINES=2`, with two host calls at once, takes about 1 minute. `MAX_OPS=3` takes about 50 seconds. `CALL_GOROUTINES=2 MAX_CALLS=3` takes about 1.5 minutes.

App calls take any item the app has, including removed ones, as the API allows. Host calls cover these methods:

- `GetLayout` on the root or any item, with depth -1.
- `GetGroupProperties` with no ids, which locks every item in `Menu.nodes`. A call with ids locks a subset in the same way.
- `GetProperty` and `Event` on any item.
- `AboutToShow`.
- `AboutToShowGroup` with one id.

`EventGroup` is a loop of `event` calls with nothing held between them, so `Event` covers it.

The script stops with `MODEL STALE` when the code no longer matches the model:

- Every lock site in the package's non-test `.go` files must match `model/lock-sites.txt`, unless it matches one of the code shapes below. A lock site is a function that takes or releases a lock, emits a D-Bus message, runs a handler or prop callback, or calls a function that does. `scripts/lock-sites` lists the steps of each one in order, including the `if` and `for` headers around them.
- godbus must be the version in `GODBUS_VERSION` in the script.

These are assumptions the script cannot read from the code:

- godbus runs each method call on its own goroutine (`go conn.handleCall(msg)` in godbus `conn.go`), and its reading goroutine takes no menu lock.
- `conn.Emit` and godbus writing a reply take `outputHandler.closed.lck` for reading and then `outputHandler.sendLck`, and nothing else, and they release both after the write (godbus `sendAndIfClosed`). Only `Conn.Close` writes `closed.lck`, and the menu never calls it under a lock. So these are not steps in the model. A write that never finishes, for example because the bus stops reading, would hold whatever menu locks the caller holds. The model assumes every write finishes.
- Changes to the menu tree happen under `Menu.m`, and so do all reads of it. So the model makes a function's tree reads and writes at the step that takes `Menu.m`.
- The modeled `MenuEventHandler` calls `SetProps` on its own item. A handler that calls something else runs the same way, with nothing held.
- `prop.Properties` locks, used by `Item.SetProps` and the `Menu` property getters, are never taken while a menu lock is held. They are separate from this model.
- Like `verify-menu-sync`, the model leaves out a move that puts an item under itself or its own child. The code takes the same locks, so the model keeps those steps but leaves the tree unchanged. The library does not check for that move, and the cycle makes `buildLayout` recurse until the stack overflows.

## Read the result

- `PASS` lists every property that held in every reachable state.
- `FAIL <property>` means a break. The script prints the steps in the code's words, one line per step, with the file and statement each comes from. Then it shows what each goroutine holds and which step it is stuck on. The full TLC trace is in `out/main.out`, and `scripts/explain-trace out/main.trace.json` prints the short form again.
- `MODEL STALE` means a lock site changed in a way the model does not describe. See the next section.
- `TOOL ERROR` means TLC did not finish. Read `out/main.out`. It is usually a model syntax or type error.

Read every result with the `/statechart` skill. It turns a violation into its shortest path in plain words and shows which violations a change fixes. On a pass it draws how the goroutines move between lock states.

## Real bug or modeling error

The script prints `code shapes` first. Each is a model constant that the script sets when a lock site differs from `model/lock-sites.txt` in that exact way.

| Constant | Set when | Property that fails |
|---|---|---|
| `ItemFirst` | `MenuItem.SetProps` or `MenuItem.AddChild` locks its item before `item.menu.lock()`, the shape #3 removed | `MenuLockedFirst` and `NoDeadlock` |
| `EventHoldsMenuRead` | `dbusmenu.event` read-locks `menu.m` before it calls `getHandler` and the handler | `HandlerRunsUnlocked`, `NoRecursiveRead`, and `NoDeadlock` |
| `ShowGroupCallsShow` | `AboutToShowGroup` calls `AboutToShow` inside its own read lock | `NoRecursiveRead` and `NoDeadlock` |

When a check fails or says `MODEL STALE`:

1. A shape constant is set, and a property fails. The change brought back a known bad shape. The failure is real. Report the printed steps.
2. `MODEL STALE` names a function, and the change was a rename, a reformat, or a lock-free edit near a lock. Compare the `recorded` and `now` lines. If the locks are the same as before, regenerate the record from the repo root with `.cursor/skills/verify-menu-locks/scripts/lock-sites . > .cursor/skills/verify-menu-locks/model/lock-sites.txt` and rerun.
3. `MODEL STALE` names a function whose locks changed on purpose. Change that function's program in `model/MenuLocks.tla` to match (see the table below), regenerate `model/lock-sites.txt` as in step 2, and rerun. Never loosen a property to make it pass.
4. `MODEL STALE` names a new lock site. Model the new function as its own program, and add a start action if app code or the host can call it directly. Then regenerate the record.
5. A godbus bump. Check the assumptions above in the new godbus source, then update `GODBUS_VERSION`.

`lock-sites` matches calls by name, so a call to a function that shares a name with a locking one shows up too. For example, `Item.createMenu` calls `Menu.export`, and that is listed as `call export` because `Item.export` locks. Such an entry is harmless.

## Where the model comes from

Each step in the model carries an `at` string naming the statement it comes from, and the printed counterexample shows it.

| Model program | Code |
|---|---|
| `SetPropsHead`, `SetPropsTail` | `MenuItem.SetProps`: `defer item.menu.lock()()`, `defer item.lock()()`, `getParent`, `emitPropertiesUpdated` |
| `AddRootHead`, `AddRootTail` | `Menu.AddChild`: `defer menu.lock()()`, `newItem`, `defer child.lock()()` |
| `AddHead`, `AddTail` | `MenuItem.AddChild`: `defer item.menu.lock()()`, `defer item.lock()()`, `newItem`, `defer child.lock()()` |
| `RemoveHead`, `RemoveTail` | `MenuItem.Remove`: `defer item.menu.lock()()`, `getParent`, `defer parent.lock()()` for an item parent |
| `MoveHead`, `MoveTail` | `appendChild` (behind both `AppendChild` methods) and `MenuItem.MoveBefore`: Menu lock, `getParent`, then `dst` and the old parent under the same conditions as the code |
| `Getter` | `MenuItem.Label`, `Type`, `Enabled`, `Visible`, `IconName`, `IconData`, `Shortcut`, `ToggleType`, `ToggleState`, `VendorProp` |
| `MenuHandler` | `ItemMenuHandler`'s prop callback, run by `Item.SetProps` |
| `GetLayoutHead`, `GetLayoutTail`, `Walk` | `dbusmenu.GetLayout` under `menu.m.Lock()`, then `buildLayout` read-locking each item while `buildChildren` walks under it |
| `GroupHead`, `GroupTail` | `dbusmenu.GetGroupProperties`: `menu.m.RLock()`, then each item's `RLock` and `RUnlock` in turn |
| `PropHead`, `PropTail` | `dbusmenu.GetProperty`: `menu.m.RLock()`, then the item's |
| `EventHead`, `EventTail` | `dbusmenu.event` and `getHandler`, then the handler |
| `ShowProg`, `ShowGroupProg` | `dbusmenu.AboutToShow` and `AboutToShowGroup` |
| `Step` | `sync.RWMutex` `Lock`, `RLock`, `Unlock`, and `RUnlock` in Go's `src/sync/rwmutex.go` |

## Findings

None. The current code passes every property within the bounds above.

## Keep it in step with the code

Sync commit: `d5e8c87` (the model was last confirmed to describe the covered code at this commit)

Covered code:

- `menu.go`: `Menu.lock`, `Menu.updateLayout`, `ItemMenuHandler`
- `menuitem.go`: `Menu.AddChild`, `MenuItem.AddChild`, `MenuItem.Remove`, `appendChild`, `MenuItem.MoveBefore`, `MenuItem.SetProps`, `MenuItem.lock`, `MenuItem.getParent`, the `MenuItem` getters
- `dbusmenu.go`: `GetLayout`, `buildLayout`, `buildChildren`, `GetGroupProperties`, `GetProperty`, `getHandler`, `event`, `Event`, `EventGroup`, `AboutToShow`, `AboutToShowGroup`
- `item.go`: `Item.SetProps`, for when prop callbacks run
- `go.mod`: the `github.com/godbus/dbus/v5` version

The script prints the covered files that changed since the sync commit. The lock-site check catches lock changes on its own, but a change to the tree logic (`getParent`, `setChildren`, `Menu.nodes`) can change which locks a function takes without changing its lock sites. After you confirm that the model still describes the code at a newer commit, move the sync commit forward to it. Never move it without that check.
