---
name: verify-menu-sync
description: Model-check that a desktop tray host's copy of a deedles.dev/tray menu ends up matching the app's Menu after any mix of AddChild, Remove, AppendChild, MoveBefore, and SetProps calls racing with the host's GetLayout and GetGroupProperties calls, using a TLA+ model whose code facts are read from menu.go, menuitem.go, and dbusmenu.go. Use when a change touches updateLayout, emitPropertiesUpdated, setChildren, Menu or MenuItem methods that change the menu, GetLayout, buildLayout, GetGroupProperties, Menu.m locking, or the godbus version, or when running /verify-menu-sync.
---

# Verify menu sync

A tray host never reads the app's `Menu` directly. It keeps its own copy, built from `GetLayout` and `GetGroupProperties` replies and patched by `LayoutUpdated` and `ItemsPropertiesUpdated` signals. The model in `model/MenuSync.tla` checks that the copy ends up matching the menu. TLC, the TLA+ model checker, tries every order of app calls, signals, and replies within the bounds below, including a menu change that lands between the moment `GetLayout` reads the menu and the moment godbus writes the reply. Tests only cover the orders someone wrote down.

The host in the model is the GNOME AppIndicator extension's `DBusClient` (`dbusMenu.js` in ubuntu/gnome-shell-extension-appindicator) with the menu open.

## Run it

From the repo root:

```bash
.cursor/skills/verify-menu-sync/scripts/check-menu-sync
```

It needs Java 11 or newer, `perl`, and `curl`. Without a system Java, `mise exec java@temurin-21 -- <command>` works. The first run downloads `tla2tools.jar` 1.8.0 to `~/.cache/deedles-tray/` and checks its sha256. 1.8.0 is a prerelease, and upstream sometimes replaces its jar. If a fresh download fails the check, compare its sha256 with the digest GitHub shows for the release asset, then update `TLA_SHA256` in both check scripts. Set `TLA2TOOLS_JAR` to use another copy. A run takes about 5 seconds. `TLC_WORKERS=2` limits the cores it uses.

To check one property alone, ignoring the known findings list:

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

## Bounds and assumptions

The script sets the bounds. Raise one with an environment variable when a change needs a deeper search: `MAX_OPS=4` takes about 3 minutes.

- `MAX_ITEMS=3`: at most 3 items are ever created, so an item can sit under another item and move between parents.
- `MAX_OPS=3`: at most 3 app calls into the menu API.
- `MAX_LABEL=2`: each item's label changes at most once after it is created.
- The host's calls are not bounded. Each `LayoutUpdated` starts one.

The model assumes these, and the script stops with `MODEL STALE` when the code no longer matches one of them:

- Every method that changes the menu takes `Menu.m` first and emits its signals before it unlocks, so each method is one step.
- `GetLayout` holds `Menu.m` while it builds the layout.
- `buildLayout` copies every property into the reply with `mapSlice(item.props, nil)`, whatever names the host asked for.
- `updateLayout` emits one `LayoutUpdated` per node it is given.

These are assumptions the script cannot read from the code:

- godbus serves each call on its own goroutine and writes the reply after the method returns (`handleCall` in godbus `export.go`). The `LateReply` constant is this, and it is TRUE in every run except the one that checks the known finding without it.
- All messages from the item reach the host in the order the item sent them, as D-Bus guarantees for one connection.
- The host behaves like the GNOME extension with its menu open. It answers `LayoutUpdated` by cancelling its last layout update, including the `GetGroupProperties` call it started, and calling `GetLayout(0, -1)`. It applies `ItemsPropertiesUpdated` only to items it already has. It replaces its copy with each `GetLayout` reply, properties included, and calls `GetGroupProperties` for the items that are new to it. With the menu closed, GNOME defers signals until the menu opens and then refetches everything, which the model does not cover.
- Every item gets a label when it is created. Other properties change the same way a label does.

## Read the result

- `PASS` lists every property that held in every reachable state.
- `FAIL <property>` means a new break. Open the `.out` file it names. Its `State N:` lines are the steps, and the variables under each step show what changed.
- `KNOWN FINDING still present` is expected for the finding below. The check exits 0 when only that fails.
- `FIXED? <property> now passes` means the model no longer finds that break. Confirm the fix in the code with a test, since a code fact can flip without the bug being gone, then remove the entry from `KNOWN_FINDINGS` in the script and from Findings below. The check exits 1 until you do. Sending `LayoutUpdated` from `SetProps` also makes the finding pass, because the host then refetches on every label change. That trade is the `LabelChangeKeepsMenuOpen` failure printed next to it.
- `MODEL STALE` means one of the assumptions above no longer matches the code. See the next section.
- `TOOL ERROR` means TLC did not finish. Read the `.out` file. It is usually a model syntax or type error.

Read every result with the `/statechart` skill. It turns a violation into its shortest path in plain words and shows which violations a change fixes. On a pass it draws how the host's copy moves between states.

## Real bug or modeling error

The script prints `code facts` first. Each is a model constant read from the code by a regex in `scripts/check-menu-sync`.

| Constant | TRUE when the code has | Property that fails when it flips |
|---|---|---|
| `RevisionBumps` | `menu.revision++` in `updateLayout` before the loop that emits `LayoutUpdated` with `menu.revision` | `RevisionsIncrease` |
| `AddChildSignalsLayout` | an unconditional call to `updateLayout` in both `Menu.AddChild` and `MenuItem.AddChild` | `LayoutConverges` |
| `RemoveSignalsLayout` | an unconditional call to `updateLayout` in `MenuItem.Remove` | `LayoutConverges` |
| `AppendSignalsLayout` | an unconditional call to `updateLayout` in `appendChild`, behind both `AppendChild` methods | `LayoutConverges` |
| `MoveBeforeSignalsLayout` | an unconditional call to `updateLayout` in `MenuItem.MoveBefore` | `LayoutConverges` |
| `SetPropsSignalsProps` | an unconditional call to `emitPropertiesUpdated` in `MenuItem.SetProps` | `PropsConverge`, in the run with `LateReply` FALSE |
| `SetPropsSignalsLayout` | any call to `updateLayout` in `MenuItem.SetProps`. FALSE is correct. | `LabelChangeKeepsMenuOpen` when TRUE |

An unconditional call is a statement at the top level of the method body, one tab deep. A call moved under an `if` turns the fact FALSE, and the model then treats the method as never sending the signal. When a condition is intended, model it as its own action instead of trusting the FALSE. The modeled host refetches the whole layout on any `LayoutUpdated`, so the facts only ask whether a method sends one, not which parent it names.

When a check fails or says `MODEL STALE`, compare the facts and assumptions with a run on `master`:

1. A fact flipped, and the change meant to remove that signal or bump. The failure is real. Report the counterexample in the code's words.
2. A fact flipped or an assumption broke by accident, for example after a rename or a reformat. Fix the regex so it matches the same code and rerun.
3. An assumption broke on purpose, for example `GetLayout` no longer locks `Menu.m` or `buildLayout` starts honoring `propertyNames`. The model no longer describes the code. Change the matching step in the model, update the assumption and its regex, and rerun. Never loosen a property to make it pass.
4. No fact changed, but a property fails. The change altered a step the model describes. Find the action in the table below, compare it with the new code, and update the model to match.

A new method that changes the menu has no fact and no action. Model it before trusting a pass.

## Where the model comes from

| Model action | Code |
|---|---|
| `AddChild(Root)` | `Menu.AddChild`: `newItem`, `applyProps`, `updateLayout(menu)`, the child's `emitPropertiesUpdated`, `setChildren` |
| `AddChild(p)` | `MenuItem.AddChild`: `setChildren`, `updateLayout(item)`, the child's `emitPropertiesUpdated`, then the parent's `children-display` |
| `SetLabel` | `MenuItem.SetProps(MenuItemLabel(...))`: `applyProps`, `emitPropertiesUpdated` |
| `RemoveItem` | `MenuItem.Remove`: the parent's `setChildren`, `delete(item.menu.nodes, ...)`, `updateLayout(parent)`, the parent's `children-display` |
| `AppendChild` | `appendChild`, behind `Menu.AppendChild` and `MenuItem.AppendChild` |
| `MoveBefore` | `MenuItem.MoveBefore` |
| `Serve` of a layout call | `dbusmenu.GetLayout` under `Menu.m`, through `buildLayout` and `buildChildren` |
| `Serve` of a props call | `dbusmenu.GetGroupProperties` under `Menu.m` |
| `Reply` | godbus `handleCall` writing the reply after the method returns |
| `Host` | `_onSignal`, `_onPropertiesUpdated`, `_layoutUpdateUpdateAsync`, `_doLayoutUpdate`, and `_requestProperties` in the GNOME extension's `dbusMenu.js` |

The model leaves out a move that puts an item under itself or its own child. The library does not check for one, and the cycle makes `buildLayout` recurse until the stack overflows.

## Findings

`PropsConverge` fails on the current code. The shortest path, in the code's words:

1. The app calls `menu.AddChild(tray.MenuItemLabel("Ticks: 1"))`. The item sends `LayoutUpdated`.
2. GNOME calls `GetLayout(0, -1)`, adds the new item from the reply, and calls `GetGroupProperties` for it.
3. `GetGroupProperties` reads `Ticks: 1` under `Menu.m` and returns. godbus has not written the reply yet.
4. The app calls `SetProps(tray.MenuItemLabel("Ticks: 2"))`. The item sends `ItemsPropertiesUpdated` with `Ticks: 2`.
5. godbus writes the `GetGroupProperties` reply with `Ticks: 1`.
6. GNOME applies `Ticks: 2`, then the reply's `Ticks: 1`. The open menu shows `Ticks: 1` until the label changes again.

A `GetLayout` reply can overtake a signal the same way and put back an old label on an item GNOME already shows. A run with `LateReply` FALSE passes, so the window between the method returning and godbus writing the reply is the whole cause.

## Keep it in step with the code

Sync commit: `8f73cef` (the model was last confirmed to describe the covered code at this commit)

Covered code:

- `menu.go`: `Menu.updateLayout`, `Menu.revision`, `Menu.lock`, `Menu.setChildren`
- `menuitem.go`: `Menu.AddChild`, `MenuItem.AddChild`, `MenuItem.Remove`, `appendChild`, `MenuItem.MoveBefore`, `MenuItem.SetProps`, `MenuItem.emitPropertiesUpdated`, `MenuItem.setChildren`, `MenuItem.getParent`
- `dbusmenu.go`: `GetLayout`, `buildLayout`, `buildChildren`, `GetGroupProperties`
- `go.mod`: the `github.com/godbus/dbus/v5` version, for `handleCall`

The script prints the covered files that changed since the sync commit. After you confirm that the model still describes the code at a newer commit, move the sync commit forward to it. Never move it without that check.
