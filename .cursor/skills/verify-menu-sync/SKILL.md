---
name: verify-menu-sync
description: Model-check that a desktop tray host's copy of a deedles.dev/tray menu ends up matching the app's Menu after any mix of AddChild, Remove, AppendChild, MoveBefore, and SetProps calls racing with the host's GetLayout and GetGroupProperties calls, including calls on a held *MenuItem pointer after Remove and moves that would create a cycle, using a TLA+ model whose code facts are read from menu.go, menuitem.go, dbusmenu.go, and item.go. Use when a change touches updateLayout, emitPropertiesUpdated, setChildren, Menu or MenuItem methods that change the menu, getParent, GetLayout, buildLayout, GetGroupProperties, AboutToShow, AboutToShowGroup, claimMenuReplies, reply, Menu.m locking, the connection options in New, or the godbus version, or when running /verify-menu-sync.
---

# Verify menu sync

A tray host never reads the app's `Menu` directly. It keeps its own copy, built from `GetLayout` and `GetGroupProperties` replies and patched by `LayoutUpdated` and `ItemsPropertiesUpdated` signals. The model in `model/MenuSync.tla` checks that the copy ends up matching the menu, and that the menu tree itself stays well formed: every id in `Menu.nodes` is reachable from the root, every live `children` id is in `Menu.nodes`, a removed item has `getParent() == nil`, walking parents from any live node reaches the root, and `AboutToShowGroup([0])` treats the root like `AboutToShow(0)`. TLC, the TLA+ model checker, tries every order of app calls, signals, and replies within the bounds below, including a menu change that lands right after `GetLayout` or `GetGroupProperties` reads the menu, a call on a `*MenuItem` the app still holds after `Remove`, and a move that would put an item under itself or a descendant. Tests only cover the orders someone wrote down.

The host in the model is the GNOME AppIndicator extension's `DBusClient` (`dbusMenu.js` in ubuntu/gnome-shell-extension-appindicator) with the menu open.

## Run it

From the repo root:

```bash
.cursor/skills/verify-menu-sync/scripts/check-menu-sync
```

It needs Java 11 or newer, `perl`, and `curl`. Without a system Java, `mise exec java@temurin-21 -- <command>` works. The first run downloads `tla2tools.jar` 1.8.0 to `~/.cache/deedles-tray/` and checks its sha256. 1.8.0 is a prerelease, and upstream sometimes replaces its jar. If a fresh download fails the check, compare its sha256 with the digest GitHub shows for the release asset, then update `TLA_SHA256` in both check scripts. Set `TLA2TOOLS_JAR` to use another copy. A run takes about 9 seconds. `TLC_WORKERS=2` limits the cores it uses.

To check one property alone, skipping the known findings list and the `LateReply` TRUE run:

```bash
.cursor/skills/verify-menu-sync/scripts/check-menu-sync --only LayoutConverges
```

Output and counterexamples go to `.cursor/skills/verify-menu-sync/out/`, which git ignores.

## What it checks

Each property is an invariant. `LayoutConverges` and `PropsConverge` are checked whenever nothing is in flight: no signal or reply on the wire, and no call the item has not answered.

| Property | Plain meaning | Harm when broken |
|---|---|---|
| `LayoutConverges` | Once things settle, the host shows the same items under the same parents in the same order as the app's menu. | An added item never appears, a removed one stays clickable, or a moved one shows in its old place. |
| `PropsConverge` | Once things settle, every item the host shows has the app's label and `children-display`. | The menu shows an old label, or an item with children does not open as a submenu. |
| `RevisionsIncrease` | Every layout change sends `LayoutUpdated` with a revision newer than every earlier one. | A host that compares the revision with the one its last `GetLayout` returned, as the dbusmenu interface describes, ignores the change and keeps the old menu. |
| `LabelChangeKeepsMenuOpen` | `MenuItem.SetProps` never sends `LayoutUpdated`. | GNOME rebuilds an open menu on `LayoutUpdated`, so every label change closes the menu under the user's pointer. |
| `NodesReachable` | Every id in `Menu.nodes` is reachable from the root through live children lists. | A child of a removed item, or a child added on a held pointer to a removed item, stays in `Menu.nodes` but is not in the tree the host can walk. |
| `NoDanglingChildIDs` | Every id in a live `children` list is in `Menu.nodes`. | `AppendChild` or `MoveBefore` on a removed item whose `getParent()` is still non-nil puts the deleted id back into a live `children` list. `GetLayout` skips it, so the host and the in-memory tree disagree about what is a child. |
| `RemovedHasNoParent` | A removed item has `getParent() == nil`. | `Remove` never clears `item.parent`, so a removed root child still has parent 0 and `getParent()` returns the `Menu`. Later `SetProps`, `Remove`, `AppendChild`, and `MoveBefore` on the held pointer run as if the item were still in the tree. |
| `Acyclic` | Walking parents from any live node reaches the root. | `AppendChild` or `MoveBefore` can put an item under itself or a descendant. `buildLayout` then recurses until the stack overflows, and the cycled items are no longer reachable from the root. |
| `AboutToShowGroupAcceptsRoot` | `AboutToShowGroup([0])` checks the root's dirty state like `AboutToShow(0)`, and does not report 0 as invalid. | The host asks about the root, gets 0 back in `invalid`, and may skip a layout refresh that `AboutToShow(0)` would have requested. |

## Bounds and assumptions

The script sets the bounds. Raise one with an environment variable when a change needs a deeper search: `MAX_OPS=4` takes about 3 minutes. Keep `MAX_OPS` at least 3, or the paths that used to break `NodesReachable` and `NoDanglingChildIDs` are not reachable.

- `MAX_ITEMS=3`: at most 3 items are ever created, so an item can sit under another item and move between parents, and a call can use a held pointer to a removed item.
- `MAX_OPS=3`: at most 3 app calls into the menu API, including calls on a held pointer after `Remove`.
- `MAX_LABEL=2`: each item's label changes at most once after it is created.
- The host's calls are not bounded. Each `LayoutUpdated` starts one.

The model assumes these, and the script stops with `MODEL STALE` when the code no longer matches one of them:

- Every method that changes the menu takes `Menu.m` first and emits its signals before it unlocks, so each method is one step.
- `GetLayout` holds `Menu.m` while it builds the layout.
- `buildLayout` copies every property into the reply with `mapSlice(item.props, nil)`, whatever names the host asked for.
- `updateLayout` emits one `LayoutUpdated` per node it is given.
- `getParent` returns the `Menu` when `item.parent == 0`.
- `AboutToShow` returns whether the id is in `menu.dirty`.

These are assumptions the script cannot read from the code:

- godbus serves each call on its own goroutine (`handleCall` in godbus `export.go`). It writes a method's reply after the method returns, unless the call has `FlagNoReplyExpected`, and it writes an error reply either way. `claimMenuReplies` sets that flag before dispatch on the calls whose method writes its own reply, so those get exactly one reply, written before the method unlocks `Menu.m`. `conn.Send` returns only after the reply is written.
- All messages from the item reach the host in the order the item sent them, as D-Bus guarantees for one connection.
- The host behaves like the GNOME extension with its menu open. It answers `LayoutUpdated` by cancelling its last layout update, including the `GetGroupProperties` call it started, and calling `GetLayout(0, -1)`. It applies `ItemsPropertiesUpdated` only to items it already has. It replaces its copy with each `GetLayout` reply, properties included, and calls `GetGroupProperties` for the items that are new to it. With the menu closed, GNOME defers signals until the menu opens and then refetches everything, which the model does not cover.
- Every item gets a label when it is created. Other properties change the same way a label does.

## Read the result

- `PASS` lists every property that held in every reachable state.
- `FAIL <property>` means a new break. Open the `.out` file it names. Its `State N:` lines are the steps, and the variables under each step show what changed.
- `PASS  PropsConverge fails with LateReply TRUE, as it must` is a control run. It forces `LateReply` TRUE whatever the code says, so it shows the model still finds the stale reply described under Findings. `FAIL  PropsConverge passes with LateReply TRUE` means the model lost that race, and a pass of the main run no longer says anything about it.
- `PASS  NodesReachable fails with RejectCycleMoves FALSE, as it must` or `PASS  Acyclic fails with RejectCycleMoves FALSE, as it must` is the other control run. It forces the cycle-move guard off, so it shows the model still finds an unguarded cycle. `FAIL  NodesReachable and Acyclic pass with RejectCycleMoves FALSE` means the model no longer attempts those moves, and a pass of the main run no longer says anything about them.
- `KNOWN FINDING still present` is expected for a property listed in `KNOWN_FINDINGS` in the script. The check exits 0 when only those fail with the live code facts, and each listed property holds when its code-fact constants are set to their fixed values.
- `FIXED? <property> now passes` means the live code facts no longer make the model find a listed break. Confirm the fix in the code with a test, since a code fact can flip without the bug being gone, then remove the entry from `KNOWN_FINDINGS` and from Findings below. The check exits 1 until you do. If you fixed the code and still see `KNOWN FINDING still present`, the regex for that fact did not match the fix: update the regex so the fact flips, rerun, and then remove the entry.
- `MODEL STALE` means one of the assumptions above no longer matches the code. See the next section.
- `TOOL ERROR` means TLC did not finish. Read the `.out` file. It is usually a model syntax or type error.

Read every result with the `/statechart` skill. It turns a violation into its shortest path in plain words and shows which violations a change fixes. On a pass it draws how the host's copy moves between states.

## Real bug or modeling error

The script prints `code facts` first. Each is a model constant read from the code by a regex in `scripts/check-menu-sync`.

| Constant | TRUE when the code has | Property that fails when it flips |
|---|---|---|
| `LateReply` | not all of these: `New` connects with `dbus.WithIncomingInterceptor(claimMenuReplies)`, `claimMenuReplies` sets `dbus.FlagNoReplyExpected \| flagReplyOwed`, `dbusmenu.send` calls `conn.Send`, and `GetLayout` and `GetGroupProperties` take a `call dbus.Message` and end with a top-level `return reply2(menu, call, ...` and `return reply(menu, call, ...`. FALSE is correct. | `PropsConverge` when TRUE |
| `RevisionBumps` | `menu.revision++` in `updateLayout` before the loop that emits `LayoutUpdated` with `menu.revision` | `RevisionsIncrease` |
| `AddChildSignalsLayout` | an unconditional call to `updateLayout` in both `Menu.AddChild` and `MenuItem.AddChild` | `LayoutConverges` |
| `RemoveSignalsLayout` | an unconditional call to `updateLayout` in `MenuItem.Remove` | `LayoutConverges` |
| `AppendSignalsLayout` | an unconditional call to `updateLayout` in `appendChild`, behind both `AppendChild` methods | `LayoutConverges` |
| `MoveBeforeSignalsLayout` | an unconditional call to `updateLayout` in `MenuItem.MoveBefore` | `LayoutConverges` |
| `SetPropsSignalsProps` | an unconditional call to `emitPropertiesUpdated` in `MenuItem.SetProps` | `PropsConverge` |
| `SetPropsSignalsLayout` | any call to `updateLayout` in `MenuItem.SetProps`. FALSE is correct. | `LabelChangeKeepsMenuOpen` when TRUE |
| `RemoveLeavesOrphans` | `MenuItem.Remove` does not walk `item.children` or `item.getChildren()` (or call `removeTree` / `removeSubtree`) to delete descendants from `Menu.nodes`. FALSE is correct. | `NodesReachable` when TRUE |
| `RemovedKeepsParent` | `getParent` does not look up `item.menu.nodes[item.id]`, and `Remove` does not assign `item.parent`. FALSE is correct. | `RemovedHasNoParent` and `NoDanglingChildIDs` when TRUE |
| `ParentMustBeLive` | `MenuItem.AddChild` looks up `item.menu.nodes[item.id]`, and `appendChild` looks up `nodes[dst`. TRUE is the current code. | `NodesReachable` when FALSE |
| `RestoreRemovedToNodes` | `appendChild` assigns `menu.nodes[child.id]` and `MoveBefore` assigns `item.menu.nodes[item.id]`. FALSE on the current code. | `NoDanglingChildIDs` stays broken when this is FALSE and `RemovedKeepsParent` is TRUE |
| `AboutToShowGroupRejectsRoot` | `AboutToShowGroup` has no `id == 0` or `id != 0` special case, so `menu.nodes[0]` is nil and 0 is reported in `invalid`. FALSE is correct. | `AboutToShowGroupAcceptsRoot` when TRUE |
| `RejectCycleMoves` | `appendChild` and `MoveBefore` call `isAncestor`, and `MoveBefore` returns before mutating when `item == sibling`. TRUE is the current code. | `NodesReachable` and `Acyclic` when FALSE |

An unconditional call is a statement at the top level of the method body, one tab deep. A call moved under an `if` turns the fact FALSE, and the model then treats the method as never sending the signal. When a condition is intended, model it as its own action instead of trusting the FALSE. The modeled host refetches the whole layout on any `LayoutUpdated`, so the facts only ask whether a method sends one, not which parent it names.

When a check fails or says `MODEL STALE`, compare the facts and assumptions with a run on `master`:

1. A fact flipped, and the change meant to remove that signal or bump, or to fix one of the known findings. The failure, or the `FIXED?` line, is real. Report the counterexample in the code's words.
2. A fact flipped or an assumption broke by accident, for example after a rename or a reformat. Fix the regex so it matches the same code and rerun.
3. An assumption broke on purpose, for example `GetLayout` no longer locks `Menu.m` or `buildLayout` starts honoring `propertyNames`. The model no longer describes the code. Change the matching step in the model, update the assumption and its regex, and rerun. Never loosen a property to make it pass.
4. No fact changed, but a property fails. The change altered a step the model describes. Find the action in the table below, compare it with the new code, and update the model to match.

A new method that changes the menu has no fact and no action. Model it before trusting a pass.

## Where the model comes from

| Model action | Code |
|---|---|
| `AddChild(Root)` | `Menu.AddChild`: `newItem`, `applyProps`, `updateLayout(menu)`, the child's `emitPropertiesUpdated`, `setChildren` |
| `AddChild(p)` | `MenuItem.AddChild` on a live or, unless `ParentMustBeLive`, a removed item: `setChildren`, `updateLayout(item)`, the child's `emitPropertiesUpdated`, then the parent's `children-display` |
| `SetLabel` | `MenuItem.SetProps(MenuItemLabel(...))`: `applyProps`, `emitPropertiesUpdated`, after `getParent` is non-nil |
| `RemoveItem` | `MenuItem.Remove`: the parent's `setChildren`, `item.removeSubtree` (clears `item.parent` and deletes the item and its descendants from `Menu.nodes`), `updateLayout(parent)`, the parent's `children-display`. |
| `AppendChild` | `appendChild`, behind `Menu.AppendChild` and `MenuItem.AppendChild`, including a held pointer to a removed child while `getParent` is non-nil, and a no-op `RejectMove` when `isAncestor(child, dst)` |
| `MoveBefore` | `MenuItem.MoveBefore`, same rule for a removed child, a no-op when `item == sibling`, and `RejectMove` when `isAncestor(item, dst)` |
| `Serve` of a layout call | `dbusmenu.GetLayout` under `Menu.m`, through `buildLayout` and `buildChildren`, then `reply2` writing the reply with `conn.Send` before the unlock |
| `Serve` of a props call | `dbusmenu.GetGroupProperties` under `Menu.m`, then `reply` writing the reply with `conn.Send` before the unlock |
| `Reply` | only with `LateReply`: godbus `handleCall` writing the reply after the method returns |
| `Host` | `_onSignal`, `_onPropertiesUpdated`, `_layoutUpdateUpdateAsync`, `_doLayoutUpdate`, and `_requestProperties` in the GNOME extension's `dbusMenu.js` |
| `GroupInvalid` | `dbusmenu.AboutToShowGroup`: ids for which `menu.nodes[id]` is nil, unless `AboutToShowGroupRejectsRoot` is FALSE, in which case id 0 is treated like `AboutToShow(0)` |

## Findings

The main run at `MAX_ITEMS=3`, `MAX_OPS=3`, `MAX_LABEL=2` checks 13,420 distinct states. `NodesReachable`, `NoDanglingChildIDs`, `RemovedHasNoParent`, `Acyclic`, and `AboutToShowGroupAcceptsRoot` now hold on the current code, along with `LayoutConverges`, `PropsConverge`, `RevisionsIncrease`, and `LabelChangeKeepsMenuOpen`. `Remove` clears `item.parent` and recursively detaches descendants, `getParent` returns nil for an item missing from `Menu.nodes`, `AddChild` and `appendChild` no-op when the destination is not live, `AboutToShowGroup` treats id 0 like `AboutToShow(0)`, and `AppendChild` and `MoveBefore` reject a move that would create a cycle. `TestRemoveDetachesDescendants`, `TestSetPropsAfterRemovedRootChild`, `TestAddChildOnRemovedItem`, `TestAppendChildAndMoveBeforeAfterRemove`, `TestRemoveAlreadyRemovedIsNoop`, `TestAboutToShowGroupAcceptsRoot`, `TestAppendChildSelfRejected`, `TestAppendChildAncestorRejected`, `TestMoveBeforeDescendantRejected`, and `TestMoveBeforeSelfIsNoop` cover those paths.

The control run with `RejectCycleMoves` FALSE shows what the cycle guard prevents: an `AppendChild` or `MoveBefore` under the item or a descendant breaks `NodesReachable` and `Acyclic`.

The control run shows what `GetLayout` and `GetGroupProperties` writing their own reply prevents. With `LateReply` TRUE, the shortest path, in the code's words:

1. The app calls `menu.AddChild(tray.MenuItemLabel("Ticks: 1"))`. The item sends `LayoutUpdated`.
2. GNOME calls `GetLayout(0, -1)`, adds the new item from the reply, and calls `GetGroupProperties` for it.
3. `GetGroupProperties` reads `Ticks: 1` under `Menu.m` and returns. godbus has not written the reply yet.
4. The app calls `SetProps(tray.MenuItemLabel("Ticks: 2"))`. The item sends `ItemsPropertiesUpdated` with `Ticks: 2`.
5. godbus writes the `GetGroupProperties` reply with `Ticks: 1`.
6. GNOME applies `Ticks: 2`, then the reply's `Ticks: 1`. The open menu shows `Ticks: 1` until the label changes again.

A `GetLayout` reply can overtake a signal the same way. In the code, step 4 waits for `Menu.m`, which `GetGroupProperties` holds until its reply is written, so the reply always comes first. `TestSetPropsBeforeUnwrittenReply` checks the same order on a real bus.

## Keep it in step with the code

Sync commit: `8284dd0` (the model was last confirmed to describe the covered code at this commit)

Covered code:

- `menu.go`: `Menu.updateLayout`, `Menu.revision`, `Menu.lock`, `Menu.setChildren`, `Menu.nodes`, `Menu.dirty`
- `menuitem.go`: `Menu.AddChild`, `MenuItem.AddChild`, `MenuItem.Remove`, `removeSubtree`, `appendChild`, `MenuItem.MoveBefore`, `MenuItem.SetProps`, `MenuItem.emitPropertiesUpdated`, `MenuItem.setChildren`, `isAncestor`, `MenuItem.getParent`
- `dbusmenu.go`: `GetLayout`, `buildLayout`, `buildChildren`, `GetGroupProperties`, `AboutToShow`, `AboutToShowGroup`, `claimMenuReplies`, `flagReplyOwed`, `replyingMethods`, `reply`, `reply2`, `dbusmenu.send`
- `item.go`: `New`, for the connection options
- `go.mod`: the `github.com/godbus/dbus/v5` version, for `handleCall`

The script prints the covered files that changed since the sync commit. After you confirm that the model still describes the code at a newer commit, move the sync commit forward to it. Never move it without that check.
