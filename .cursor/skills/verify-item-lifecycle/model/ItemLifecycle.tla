---------------------------- MODULE ItemLifecycle ----------------------------
(***************************************************************************)
(* Item, watcher, and bus lifecycle for deedles.dev/tray.                  *)
(*                                                                         *)
(* New connects to the session bus with claimMenuReplies, requests a       *)
(* well-known name (or falls back to the unique name), exports the         *)
(* StatusNotifierItem interfaces, properties, introspect data, and the     *)
(* dbusmenu, may emit property signals from the initial SetProps, then     *)
(* registers with org.freedesktop.StatusNotifierWatcher and, if that       *)
(* fails, org.kde.StatusNotifierWatcher. First success wins. If both       *)
(* fail, or any earlier export step fails, New returns an error. On the    *)
(* current code, that error path does not call conn.Close(), so the        *)
(* connection and any requested name stay open.                            *)
(*                                                                         *)
(* After a successful New, the app can SetProps, the host can call         *)
(* Activate, SecondaryActivate, ContextMenu, and Scroll, the watcher that  *)
(* accepted the item can disappear and come back, and Close closes the     *)
(* connection. The current code does not re-register when the watcher      *)
(* returns.                                                                *)
(*                                                                         *)
(* File references are to the repo root: item.go, menu.go, and             *)
(* statusnotifieritem.go. The constants other than the bounds are code     *)
(* facts that the check script reads from the code.                        *)
(***************************************************************************)
EXTENDS Naturals

CONSTANTS
  MaxPropSets,   \* SetProps calls after New returns
  MaxHostCalls,  \* host Activate / SecondaryActivate / ContextMenu / Scroll calls
  MaxRestarts,   \* times the watcher that accepted the item can disappear
  \* Code facts. scripts/check-item-lifecycle reads each one from the code.
  NewClosesConnOnErr,         \* New's post-connect error returns call conn.Close
  RegisterAfterExport,        \* export() registers only after interfaces, props,
                              \* introspect, menu, and the initial SetProps
  CloseCallsConnClose,        \* Item.Close returns item.conn.Close()
  ReregistersOnWatcherRestart \* a watcher NameOwnerChanged / WatchName path
                              \* calls RegisterStatusNotifierItem again

VARIABLES
  pc,                 \* New / Close progress
  connOpen,           \* the session bus connection is open
  nameKind,           \* none, wellknown, or unique
  exportedIfaces,     \* StatusNotifierItem methods are exported
  exportedProps,      \* item properties are exported
  exportedIntrospect, \* item introspect data is exported
  exportedMenu,       \* createMenu / Menu.export succeeded
  emittedBeforeReg,   \* initial SetProps emitted a property signal
  registerAttempted,  \* RegisterStatusNotifierItem was called
  registeredOn,       \* watcher that accepted the item: none, fd, or kde
  listed,             \* that watcher's current instance has the item
  fdUp,               \* org.freedesktop.StatusNotifierWatcher is on the bus
  kdeUp,              \* org.kde.StatusNotifierWatcher is on the bus
  restarts,           \* watcher drops so far after a successful New
  propSets,           \* SetProps calls after New returned
  hostCalls           \* host method calls so far

vars == <<
  pc, connOpen, nameKind, exportedIfaces, exportedProps,
  exportedIntrospect, exportedMenu, emittedBeforeReg, registerAttempted,
  registeredOn, listed, fdUp, kdeUp, restarts, propSets, hostCalls
>>

PcOK ==
  pc \in {"start", "connected", "named", "ifaces", "props",
          "introspect", "menu", "inited", "earlyreg", "ready",
          "failed", "closed", "noconnect"}

NameOK == nameKind \in {"none", "wellknown", "unique"}

WatcherOK == registeredOn \in {"none", "fd", "kde"}

\* The item is in the current instance of the watcher that accepted it.
LiveReg ==
  \/ (listed /\ registeredOn = "fd" /\ fdUp)
  \/ (listed /\ registeredOn = "kde" /\ kdeUp)

ExportedAll ==
  exportedIfaces /\ exportedProps /\ exportedIntrospect /\ exportedMenu

\* New's error path after ConnectSessionBus. item.go New, the returns
\* after initProtoData and export.
FailNew ==
  /\ pc' = "failed"
  /\ IF NewClosesConnOnErr
     THEN /\ connOpen' = FALSE
          /\ nameKind' = "none"
          /\ registeredOn' = "none"
          /\ listed' = FALSE
     ELSE UNCHANGED <<connOpen, nameKind, registeredOn, listed>>

\* ---------------- New (item.go) ----------------

\* item.go New: dbus.ConnectSessionBus(dbus.WithIncomingInterceptor(claimMenuReplies))
ConnectOK ==
  /\ pc = "start"
  /\ pc' = "connected"
  /\ connOpen' = TRUE
  /\ UNCHANGED <<nameKind, exportedIfaces, exportedProps, exportedIntrospect,
                 exportedMenu, emittedBeforeReg, registerAttempted,
                 registeredOn, listed, fdUp, kdeUp, restarts, propSets, hostCalls>>

ConnectFail ==
  /\ pc = "start"
  /\ pc' = "noconnect"
  /\ UNCHANGED <<connOpen, nameKind, exportedIfaces, exportedProps,
                 exportedIntrospect, exportedMenu, emittedBeforeReg,
                 registerAttempted, registeredOn, listed, fdUp, kdeUp,
                 restarts, propSets, hostCalls>>

\* item.go initProtoData: RequestName of org.freedesktop.StatusNotifierItem-<pid>-<n>
RequestWellKnown ==
  /\ pc = "connected"
  /\ pc' = "named"
  /\ nameKind' = "wellknown"
  /\ UNCHANGED <<connOpen, exportedIfaces, exportedProps, exportedIntrospect,
                 exportedMenu, emittedBeforeReg, registerAttempted,
                 registeredOn, listed, fdUp, kdeUp, restarts, propSets, hostCalls>>

\* initProtoData: RequestName fails or is not PrimaryOwner; fall back to
\* item.conn.Names()[0]. The error is non-fatal; initProtoData returns nil.
RequestFallback ==
  /\ pc = "connected"
  /\ pc' = "named"
  /\ nameKind' = "unique"
  /\ UNCHANGED <<connOpen, exportedIfaces, exportedProps, exportedIntrospect,
                 exportedMenu, emittedBeforeReg, registerAttempted,
                 registeredOn, listed, fdUp, kdeUp, restarts, propSets, hostCalls>>

\* When RegisterAfterExport is FALSE, export() calls
\* RegisterStatusNotifierItem before the export steps, right after the name
\* is acquired. After a successful early register, New continues exporting.
AfterName == IF RegisterAfterExport THEN "named" ELSE "earlyreg"

ExportIfacesOK ==
  /\ pc = AfterName
  /\ pc' = "ifaces"
  /\ exportedIfaces' = TRUE
  /\ UNCHANGED <<connOpen, nameKind, exportedProps, exportedIntrospect,
                 exportedMenu, emittedBeforeReg, registerAttempted,
                 registeredOn, listed, fdUp, kdeUp, restarts, propSets, hostCalls>>

ExportIfacesFail ==
  /\ pc = AfterName
  /\ FailNew
  /\ UNCHANGED <<exportedIfaces, exportedProps, exportedIntrospect,
                 exportedMenu, emittedBeforeReg, registerAttempted,
                 fdUp, kdeUp, restarts, propSets, hostCalls>>

ExportPropsOK ==
  /\ pc = "ifaces"
  /\ pc' = "props"
  /\ exportedProps' = TRUE
  /\ UNCHANGED <<connOpen, nameKind, exportedIfaces, exportedIntrospect,
                 exportedMenu, emittedBeforeReg, registerAttempted,
                 registeredOn, listed, fdUp, kdeUp, restarts, propSets, hostCalls>>

ExportPropsFail ==
  /\ pc = "ifaces"
  /\ FailNew
  /\ UNCHANGED <<exportedIfaces, exportedProps, exportedIntrospect,
                 exportedMenu, emittedBeforeReg, registerAttempted,
                 fdUp, kdeUp, restarts, propSets, hostCalls>>

ExportIntrospectOK ==
  /\ pc = "props"
  /\ pc' = "introspect"
  /\ exportedIntrospect' = TRUE
  /\ UNCHANGED <<connOpen, nameKind, exportedIfaces, exportedProps,
                 exportedMenu, emittedBeforeReg, registerAttempted,
                 registeredOn, listed, fdUp, kdeUp, restarts, propSets, hostCalls>>

ExportIntrospectFail ==
  /\ pc = "props"
  /\ FailNew
  /\ UNCHANGED <<exportedIfaces, exportedProps, exportedIntrospect,
                 exportedMenu, emittedBeforeReg, registerAttempted,
                 fdUp, kdeUp, restarts, propSets, hostCalls>>

\* menu.go createMenu: Menu.export of dbusmenu methods, props, and introspect.
CreateMenuOK ==
  /\ pc = "introspect"
  /\ pc' = "menu"
  /\ exportedMenu' = TRUE
  /\ UNCHANGED <<connOpen, nameKind, exportedIfaces, exportedProps,
                 exportedIntrospect, emittedBeforeReg, registerAttempted,
                 registeredOn, listed, fdUp, kdeUp, restarts, propSets, hostCalls>>

CreateMenuFail ==
  /\ pc = "introspect"
  /\ FailNew
  /\ UNCHANGED <<exportedIfaces, exportedProps, exportedIntrospect,
                 exportedMenu, emittedBeforeReg, registerAttempted,
                 fdUp, kdeUp, restarts, propSets, hostCalls>>

\* item.go export: the initial SetProps, which may emit NewTitle / NewIcon
\* and the other property signals before any RegisterStatusNotifierItem call.
InitSetPropsOK ==
  /\ pc = "menu"
  /\ pc' = "inited"
  /\ emittedBeforeReg' = TRUE
  /\ UNCHANGED <<connOpen, nameKind, exportedIfaces, exportedProps,
                 exportedIntrospect, exportedMenu, registerAttempted,
                 registeredOn, listed, fdUp, kdeUp, restarts, propSets, hostCalls>>

InitSetPropsFail ==
  /\ pc = "menu"
  /\ FailNew
  /\ UNCHANGED <<exportedIfaces, exportedProps, exportedIntrospect,
                 exportedMenu, emittedBeforeReg, registerAttempted,
                 fdUp, kdeUp, restarts, propSets, hostCalls>>

\* After a successful early register, New finishes once the export steps have.
FinishEarlyReg ==
  /\ ~RegisterAfterExport
  /\ pc = "inited"
  /\ registerAttempted
  /\ registeredOn # "none"
  /\ pc' = "ready"
  /\ UNCHANGED <<connOpen, nameKind, exportedIfaces, exportedProps,
                 exportedIntrospect, exportedMenu, emittedBeforeReg,
                 registerAttempted, registeredOn, listed, fdUp, kdeUp,
                 restarts, propSets, hostCalls>>

\* RegisterStatusNotifierItem is tried after the initial SetProps when
\* RegisterAfterExport is TRUE, and right after the name is acquired when
\* it is FALSE.
CanRegister ==
  /\ ~registerAttempted
  /\ nameKind # "none"
  /\ IF RegisterAfterExport THEN pc = "inited" ELSE pc = "named"

\* First success wins: freedesktop, then kde. item.go export's loop over spaces.
RegisterFDWins ==
  /\ CanRegister
  /\ fdUp
  /\ registerAttempted' = TRUE
  /\ registeredOn' = "fd"
  /\ listed' = TRUE
  /\ pc' = IF RegisterAfterExport THEN "ready" ELSE "earlyreg"
  /\ UNCHANGED <<connOpen, nameKind, exportedIfaces, exportedProps,
                 exportedIntrospect, exportedMenu, emittedBeforeReg,
                 fdUp, kdeUp, restarts, propSets, hostCalls>>

RegisterKDEWins ==
  /\ CanRegister
  /\ ~fdUp
  /\ kdeUp
  /\ registerAttempted' = TRUE
  /\ registeredOn' = "kde"
  /\ listed' = TRUE
  /\ pc' = IF RegisterAfterExport THEN "ready" ELSE "earlyreg"
  /\ UNCHANGED <<connOpen, nameKind, exportedIfaces, exportedProps,
                 exportedIntrospect, exportedMenu, emittedBeforeReg,
                 fdUp, kdeUp, restarts, propSets, hostCalls>>

\* Both watcher names fail. New returns errors.Join of the two register errors.
RegisterBothMiss ==
  /\ CanRegister
  /\ ~fdUp
  /\ ~kdeUp
  /\ registerAttempted' = TRUE
  /\ FailNew
  /\ UNCHANGED <<exportedIfaces, exportedProps, exportedIntrospect,
                 exportedMenu, emittedBeforeReg, fdUp, kdeUp, restarts,
                 propSets, hostCalls>>

\* ---------------- After New returns ----------------

\* item.go SetProps and emit, once the caller has a live Item.
AppSetProps ==
  /\ pc = "ready"
  /\ propSets < MaxPropSets
  /\ propSets' = propSets + 1
  /\ UNCHANGED <<pc, connOpen, nameKind, exportedIfaces, exportedProps,
                 exportedIntrospect, exportedMenu, emittedBeforeReg,
                 registerAttempted, registeredOn, listed, fdUp, kdeUp,
                 restarts, hostCalls>>

\* statusnotifieritem.go ContextMenu, Activate, SecondaryActivate, Scroll.
HostContextMenu ==
  /\ pc = "ready"
  /\ hostCalls < MaxHostCalls
  /\ hostCalls' = hostCalls + 1
  /\ UNCHANGED <<pc, connOpen, nameKind, exportedIfaces, exportedProps,
                 exportedIntrospect, exportedMenu, emittedBeforeReg,
                 registerAttempted, registeredOn, listed, fdUp, kdeUp,
                 restarts, propSets>>

HostActivate ==
  /\ pc = "ready"
  /\ hostCalls < MaxHostCalls
  /\ hostCalls' = hostCalls + 1
  /\ UNCHANGED <<pc, connOpen, nameKind, exportedIfaces, exportedProps,
                 exportedIntrospect, exportedMenu, emittedBeforeReg,
                 registerAttempted, registeredOn, listed, fdUp, kdeUp,
                 restarts, propSets>>

HostSecondaryActivate ==
  /\ pc = "ready"
  /\ hostCalls < MaxHostCalls
  /\ hostCalls' = hostCalls + 1
  /\ UNCHANGED <<pc, connOpen, nameKind, exportedIfaces, exportedProps,
                 exportedIntrospect, exportedMenu, emittedBeforeReg,
                 registerAttempted, registeredOn, listed, fdUp, kdeUp,
                 restarts, propSets>>

HostScroll ==
  /\ pc = "ready"
  /\ hostCalls < MaxHostCalls
  /\ hostCalls' = hostCalls + 1
  /\ UNCHANGED <<pc, connOpen, nameKind, exportedIfaces, exportedProps,
                 exportedIntrospect, exportedMenu, emittedBeforeReg,
                 registerAttempted, registeredOn, listed, fdUp, kdeUp,
                 restarts, propSets>>

ActiveUp ==
  \/ (registeredOn = "fd" /\ fdUp)
  \/ (registeredOn = "kde" /\ kdeUp)

\* The watcher that accepted the item leaves the bus. Its replacement, if
\* any, starts with an empty registered list.
WatcherDrops ==
  /\ pc = "ready"
  /\ listed
  /\ ActiveUp
  /\ restarts < MaxRestarts
  /\ restarts' = restarts + 1
  /\ listed' = FALSE
  /\ fdUp' = IF registeredOn = "fd" THEN FALSE ELSE fdUp
  /\ kdeUp' = IF registeredOn = "kde" THEN FALSE ELSE kdeUp
  /\ UNCHANGED <<pc, connOpen, nameKind, exportedIfaces, exportedProps,
                 exportedIntrospect, exportedMenu, emittedBeforeReg,
                 registerAttempted, registeredOn, propSets, hostCalls>>

\* That watcher name is claimed again. The current code does not call
\* RegisterStatusNotifierItem. When ReregistersOnWatcherRestart, this
\* step re-registers in the same step as the name coming back, as a
\* NameOwnerChanged handler would.
WatcherReturns ==
  /\ pc = "ready"
  /\ registeredOn # "none"
  /\ ~ActiveUp
  /\ listed' = ReregistersOnWatcherRestart
  /\ fdUp' = IF registeredOn = "fd" THEN TRUE ELSE fdUp
  /\ kdeUp' = IF registeredOn = "kde" THEN TRUE ELSE kdeUp
  /\ UNCHANGED <<pc, connOpen, nameKind, exportedIfaces, exportedProps,
                 exportedIntrospect, exportedMenu, emittedBeforeReg,
                 registerAttempted, registeredOn, restarts, propSets, hostCalls>>

\* item.go Close: return item.conn.Close(). Behavior of later calls is
\* undefined and is not modeled.
CloseItem ==
  /\ pc = "ready"
  /\ pc' = "closed"
  /\ IF CloseCallsConnClose
     THEN /\ connOpen' = FALSE
          /\ nameKind' = "none"
          /\ listed' = FALSE
          /\ registeredOn' = "none"
     ELSE UNCHANGED <<connOpen, nameKind, listed, registeredOn>>
  /\ UNCHANGED <<exportedIfaces, exportedProps, exportedIntrospect,
                 exportedMenu, emittedBeforeReg, registerAttempted,
                 fdUp, kdeUp, restarts, propSets, hostCalls>>

Done ==
  /\ pc \in {"noconnect", "failed", "closed"}
  /\ UNCHANGED vars

Init ==
  /\ pc = "start"
  /\ connOpen = FALSE
  /\ nameKind = "none"
  /\ exportedIfaces = FALSE
  /\ exportedProps = FALSE
  /\ exportedIntrospect = FALSE
  /\ exportedMenu = FALSE
  /\ emittedBeforeReg = FALSE
  /\ registerAttempted = FALSE
  /\ registeredOn = "none"
  /\ listed = FALSE
  /\ fdUp \in BOOLEAN
  /\ kdeUp \in BOOLEAN
  /\ restarts = 0
  /\ propSets = 0
  /\ hostCalls = 0

Next ==
  \/ ConnectOK
  \/ ConnectFail
  \/ RequestWellKnown
  \/ RequestFallback
  \/ ExportIfacesOK
  \/ ExportIfacesFail
  \/ ExportPropsOK
  \/ ExportPropsFail
  \/ ExportIntrospectOK
  \/ ExportIntrospectFail
  \/ CreateMenuOK
  \/ CreateMenuFail
  \/ InitSetPropsOK
  \/ InitSetPropsFail
  \/ FinishEarlyReg
  \/ RegisterFDWins
  \/ RegisterKDEWins
  \/ RegisterBothMiss
  \/ AppSetProps
  \/ HostContextMenu
  \/ HostActivate
  \/ HostSecondaryActivate
  \/ HostScroll
  \/ WatcherDrops
  \/ WatcherReturns
  \/ CloseItem
  \/ Done

Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ PcOK
  /\ connOpen \in BOOLEAN
  /\ NameOK
  /\ exportedIfaces \in BOOLEAN
  /\ exportedProps \in BOOLEAN
  /\ exportedIntrospect \in BOOLEAN
  /\ exportedMenu \in BOOLEAN
  /\ emittedBeforeReg \in BOOLEAN
  /\ registerAttempted \in BOOLEAN
  /\ WatcherOK
  /\ listed \in BOOLEAN
  /\ fdUp \in BOOLEAN
  /\ kdeUp \in BOOLEAN
  /\ restarts \in 0..MaxRestarts
  /\ propSets \in 0..MaxPropSets
  /\ hostCalls \in 0..MaxHostCalls

\* If New returns an error after connecting, no live bus connection or
\* requested name is left open.
FailedNewClosesConn ==
  pc = "failed" => (~connOpen /\ nameKind = "none")

\* RegisterStatusNotifierItem is never called before the interfaces,
\* properties, introspect data, and menu are exported.
ExportBeforeRegister ==
  registerAttempted => ExportedAll

\* After Close(), the bus connection is closed and can no longer hold a name.
CloseTearsDown ==
  pc = "closed" => (~connOpen /\ nameKind = "none")

\* When the watcher that accepted the item is on the bus, a live Item is
\* still in that watcher's registered list, including after the watcher
\* has disappeared and come back.
RegisteredSurvivesWatcherRestart ==
  (pc = "ready" /\ ActiveUp) => listed

\* A terminal state still has a step (Done), and every other state has
\* at least one real step.
NoDeadlock == ENABLED(Next)

=============================================================================
