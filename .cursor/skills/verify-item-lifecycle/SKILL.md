---
name: verify-item-lifecycle
description: Model-check that deedles.dev/tray's Item lifecycle closes the bus connection when New fails after connecting, never calls RegisterStatusNotifierItem before the item and menu are exported, tears the connection down on Close, and stays registered if the StatusNotifierWatcher disappears and comes back, using a TLA+ model whose code facts are read from item.go, menu.go, and statusnotifieritem.go. Use when a change touches New, initProtoData, export, exportProps, exportIntrospect, createMenu, Menu.export, SetProps, emit, Close, the StatusNotifierItem host methods, watcher registration, watchWatcher, or the connection options in New, or when running /verify-item-lifecycle.
---

# Verify item lifecycle

`New` opens a session bus connection, takes a name, exports the item and its menu, may emit property signals, and only then registers with a StatusNotifierWatcher. After a successful register, it also watches `NameOwnerChanged` for the watcher names and re-registers when a new owner appears. `Close` stops that watch and closes the connection. The model in `model/ItemLifecycle.tla` checks that a failed `New` does not leave the connection or a requested name open, that registration never starts before the exports, that `Close` tears the connection down, and that a live item is still registered if the watcher that accepted it disappears and comes back. TLC, the TLA+ model checker, tries every order of those steps within the bounds below. Tests only cover the orders someone wrote down.

## Run it

From the repo root:

```bash
.cursor/skills/verify-item-lifecycle/scripts/check-item-lifecycle
```

It needs Java 11 or newer, `perl`, and `curl`. Without a system Java, `mise exec java@temurin-21 -- <command>` works. The first run downloads `tla2tools.jar` 1.8.0 to `~/.cache/deedles-tray/` and checks its sha256 (`fdffef247f563ef6edbfd44958d2a82db79e7564e07cb673262d896353fa6713`, the digest GitHub currently shows for the v1.8.0 asset). 1.8.0 is a prerelease, and upstream sometimes replaces its jar. If a fresh download fails the check, compare its sha256 with the digest GitHub shows for the release asset, then update `TLA_SHA256` in all check scripts. `verify-menu-sync` and `verify-menu-locks` use the same cache path and may still have an older digest. Set `TLA2TOOLS_JAR` to use another copy. A run takes a few seconds. `TLC_WORKERS=2` limits the cores it uses. Formal checks are not part of CI. Run the script locally after changing the covered code.

To check one property alone, skipping the known findings list and the control runs:

```bash
.cursor/skills/verify-item-lifecycle/scripts/check-item-lifecycle --only ExportBeforeRegister
```

Output and counterexamples go to `.cursor/skills/verify-item-lifecycle/out/`, which git ignores.

Read every TLC result with the pstack `/statechart` skill. It shows each invariant violation as its shortest path in plain words, and when the check passes it draws how the system moves between states.

## What it checks

Each property is an invariant over every reachable state.

| Property | Plain meaning | Harm when broken |
|---|---|---|
| `FailedNewClosesConn` | If `New` returns an error after `ConnectSessionBus` succeeded, the session bus connection is closed and no well-known or unique name is still held. | The caller never gets an `*Item`, so it cannot call `Close`. The process keeps a session bus connection and may keep `org.freedesktop.StatusNotifierItem-<pid>-<n>`, and a half-exported item can stay on the bus. |
| `ExportBeforeRegister` | `RegisterStatusNotifierItem` is never called before the StatusNotifierItem interfaces, properties, and introspect data, and the dbusmenu, are exported. | A watcher or host finds the item, then looks up `/StatusNotifierItem` or `/StatusNotifierMenu` and finds nothing. |
| `CloseTearsDown` | After `Close()`, the bus connection is closed and no requested name is still held. | `Close` returns, the caller thinks the item is gone, and the name and icon stay on the bus. |
| `RegisteredSurvivesWatcherRestart` | When the watcher that accepted the item is on the bus, a live `Item` is in that watcher's registered list, including after that watcher has disappeared and come back. | The tray icon vanishes when the watcher restarts and never comes back, even though the process is still running. |
| `NoDeadlock` | Every state has a next step. Terminal states take a no-op `Done` step. | Not a product bug by itself; it means the model got stuck and a pass of the other properties no longer covers every path. |

`TypeOK` always runs with them.

## Bounds and assumptions

The script sets the bounds. Raise one with an environment variable when a change needs a deeper search.

- `MAX_PROP_SETS=1`: at most one `SetProps` after `New` returns.
- `MAX_HOST_CALLS=1`: at most one host call among `Activate`, `SecondaryActivate`, `ContextMenu`, and `Scroll`.
- `MAX_RESTARTS=1`: the watcher that accepted the item can disappear once and come back once.

The model starts with every combination of the two watcher names being on the bus or not, then runs one `New`. Watchers do not appear or disappear while `New` is still running. After a successful `New`, only the watcher that accepted the item can drop and return. The other watcher, if it was also up, stays as it was.

The model assumes these, and the script stops with `MODEL STALE` when the code no longer matches one of them:

- `New` connects with `dbus.ConnectSessionBus(dbus.WithIncomingInterceptor(claimMenuReplies))`.
- `initProtoData` treats a failed `RequestName` as non-fatal, falls back to `item.conn.Names()[0]`, and returns nil.
- `export` tries `org.freedesktop.StatusNotifierWatcher` then `org.kde.StatusNotifierWatcher`, keeps the first success, and returns an error only if both fail.
- `createMenu` calls `Menu.export`.

These are assumptions the script cannot read from the code:

- `ConnectSessionBus` is the only step that opens the connection, and `conn.Close()` releases every name the connection owned.
- A failed `conn.Export`, `prop.Export`, `createMenu`, or initial `SetProps` can happen after the connection is up, so each of those steps has a failure action.
- `initProtoData` cannot fail `New`. A `RequestName` error only chooses the unique name.
- Exporting both StatusNotifierItem interfaces is one step, and `createMenu` (dbusmenu methods, properties, and introspect) is one step. A failure part way through either call still takes the failure action.
- The initial `SetProps` in `export` emits at least one property signal, as it does when the caller passes a dirty property such as an ID or icon. Registration has not happened yet, so those signals can leave the item before any watcher knows about it.
- Host method calls run only after `New` returns. Behavior of `SetProps`, host methods, or a second `Close` after `Close` is undefined in the docs and is not modeled.
- When the watcher that accepted the item disappears, its replacement starts with an empty registered list. A `NameOwnerChanged` handler that re-registers is modeled as the same step as the name coming back.

## Read the result

- `PASS` lists every property that held in every reachable state.
- `FAIL <property>` means a new break. Open the `.out` file it names. Its `State N:` lines are the steps, and the variables under each step show what changed.
- `KNOWN FINDING still present` is for entries still listed in `KNOWN_FINDINGS` in the script. There are currently none. The check exits 0 when only those fail.
- `FIXED? <property> now passes` means the model no longer finds that break with the fact FALSE, or the code fact for the fix is already TRUE. Confirm the fix in the code with a test, since a code fact can flip without the bug being gone. Then remove the entry from `KNOWN_FINDINGS` in the script and from Findings below. The check exits 1 until you do.
- `PASS  ExportBeforeRegister fails with RegisterAfterExport FALSE, as it must`, `PASS  CloseTearsDown fails with CloseCallsConnClose FALSE, as it must`, and `PASS  RegisteredSurvivesWatcherRestart fails with ReregistersOnWatcherRestart FALSE, as it must` are the control runs. They force those facts FALSE whatever the code says, so they show the model still finds those breaks. A `FAIL` on any of them means the model lost that path, and a pass of the main run no longer says anything about it.
- `MODEL STALE` means one of the assumptions above no longer matches the code. See the next section.
- `TOOL ERROR` means TLC did not finish. Read the `.out` file. It is usually a model syntax or type error.

## Real bug or modeling error

The script prints `code facts` first. Each is a model constant read from the code by a regex in `scripts/check-item-lifecycle`.

| Constant | TRUE when the code has | Property that fails when it flips |
|---|---|---|
| `NewClosesConnOnErr` | a close-on-error `defer` after `ConnectSessionBus` in `New`, or both post-connect error returns (`initProtoData` and `export`) calling `item.Close`, `item.conn.Close`, or `conn.Close` before they return. TRUE is the current code. | `FailedNewClosesConn` when FALSE |
| `RegisterAfterExport` | `export` calls `createMenu`, then `SetProps`, then `registerWithWatchers`, `registerWithWatchers` calls `RegisterStatusNotifierItem`, and `export` does not register before `createMenu` | `ExportBeforeRegister` when FALSE |
| `CloseCallsConnClose` | `Item.Close` calls `item.conn.Close()` | `CloseTearsDown` when FALSE |
| `ReregistersOnWatcherRestart` | `item.go` has a `NameOwnerChanged` or `WatchName` watch, and a method other than `export` calls `RegisterStatusNotifierItem`. TRUE is the current code. | `RegisteredSurvivesWatcherRestart` when FALSE |

When a check fails or says `MODEL STALE`, compare the facts and assumptions with a run on `master`:

1. A fact flipped, and the change meant to add or remove that close, reorder, or re-register. The failure, or the `FIXED?` line, is real. Report the counterexample in the code's words, or update `KNOWN_FINDINGS` and Findings after confirming the fix with a test.
2. A fact flipped or an assumption broke by accident, for example after a rename or a reformat. Fix the regex so it matches the same code and rerun.
3. An assumption broke on purpose, for example `RequestName` became fatal or the watcher names changed. The model no longer describes the code. Change the matching step in the model, update the assumption and its regex, and rerun. Never loosen a property to make it pass.
4. No fact changed, but a property fails. The change altered a step the model describes. Find the action in the table below, compare it with the new code, and update the model to match.

A new way for `New` to fail after connecting, or a new path that re-registers with a watcher, has no fact and no action until you model it. Model it before trusting a pass.

## Where the model comes from

| Model action | Code |
|---|---|
| `ConnectOK`, `ConnectFail` | `New`: `dbus.ConnectSessionBus(dbus.WithIncomingInterceptor(claimMenuReplies))` |
| `RequestWellKnown`, `RequestFallback` | `initProtoData`: `RequestName`, or the fallback to `item.conn.Names()[0]` |
| `ExportIfacesOK`, `ExportIfacesFail` | `export`: `conn.Export` of both StatusNotifierItem interfaces |
| `ExportPropsOK`, `ExportPropsFail` | `exportProps` |
| `ExportIntrospectOK`, `ExportIntrospectFail` | `exportIntrospect` |
| `CreateMenuOK`, `CreateMenuFail` | `createMenu` → `Menu.export` of dbusmenu methods, properties, and introspect |
| `InitSetPropsOK`, `InitSetPropsFail` | the initial `SetProps` in `export`, which may emit property signals before registration |
| `RegisterFDWins`, `RegisterKDEWins`, `RegisterBothMiss` | `registerWithWatchers`, called from `export` after the initial `SetProps` |
| `FinishEarlyReg` | only with `RegisterAfterExport` FALSE: `New` finishes after a successful early register and the later export steps |
| `AppSetProps` | `Item.SetProps` and `emit` after `New` returns |
| `HostActivate`, `HostSecondaryActivate`, `HostContextMenu`, `HostScroll` | `statusnotifieritem.go` methods, which call the `Handler` if one is set |
| `WatcherDrops`, `WatcherReturns` | the watcher that accepted the item leaving the bus and later being claimed again. `WatcherReturns` models the `NameOwnerChanged` handler in `watchWatcher` / `onWatcherNameOwnerChanged` as the same step as the name coming back |
| `CloseItem` | `Item.Close` |
| `Done` | no further product step; keeps terminal states from deadlocking the check |

The model leaves out a second `New` on the same process, `ReleaseName` without `Close`, and hosts other than a StatusNotifierWatcher that already accepted the item.

## Findings

There are no known findings. All five properties, plus `TypeOK`, hold in these bounds. The main run checks 181 distinct states.

`FailedNewClosesConn` holds because `New` defers `item.Close()` when `initProtoData`, `export`, or `watchWatcher` returns an error after `ConnectSessionBus` succeeded. `TestFailedNewClosesConn` checks that a failed `New` with no StatusNotifierWatcher on the bus leaves no session-bus names behind.

`RegisteredSurvivesWatcherRestart` holds because, after a successful register, `watchWatcher` subscribes to `NameOwnerChanged` for both watcher names and `onWatcherNameOwnerChanged` calls `registerWithWatchers` when a new owner appears. `TestItemReregistersWhenWatcherRestarts` checks that a live `Item` registers again with a replacement `org.kde.StatusNotifierWatcher`. `TestCloseStopsWatcherWatch` checks that `Close` is safe to call twice and that a later watcher restart does not re-register.

`ExportBeforeRegister`, `CloseTearsDown`, and `NoDeadlock` also hold.

## Keep it in step with the code

Sync commit: `34ad2bc` (the model was last confirmed to describe the covered code at this commit)

Covered code:

- `item.go`: `New`, `initProtoData`, `export`, `registerWithWatchers`, `watchWatcher`, `serveWatcherWatch`, `onWatcherNameOwnerChanged`, `exportProps`, `exportIntrospect`, `SetProps`, `emit`, `Close`, `getName`
- `menu.go`: `createMenu`, `Menu.export`, `Menu.exportProps`, `Menu.exportIntrospect`
- `statusnotifieritem.go`: `ContextMenu`, `Activate`, `SecondaryActivate`, `Scroll`

The script prints the covered files that changed since the sync commit. After you confirm that the model still describes the code at a newer commit, move the sync commit forward to it. Never move it without that check.
