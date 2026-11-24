------------------------------ MODULE MenuSync ------------------------------
(***************************************************************************)
(* How a desktop tray host keeps its copy of a deedles.dev/tray menu in    *)
(* step with the app's Menu. The app changes the menu through the Menu and *)
(* MenuItem methods. Each of those runs under Menu.m and emits its         *)
(* signals before it unlocks, so each is one step here. The host is the    *)
(* GNOME AppIndicator extension's DBusClient (dbusMenu.js) with its menu   *)
(* open: LayoutUpdated cancels any GetLayout in flight and sends           *)
(* GetLayout(0, -1). ItemsPropertiesUpdated patches items it already       *)
(* knows. A GetLayout reply replaces its whole copy, properties included,  *)
(* and asks GetGroupProperties for the items that are new to it.           *)
(*                                                                         *)
(* App calls are allowed on a held *MenuItem pointer after Remove, as the  *)
(* Go API allows. Remove is not recursive, does not clear item.parent,     *)
(* and AppendChild/MoveBefore may put a removed id back into a live        *)
(* children list without putting it back into Menu.nodes, unless the       *)
(* matching code-fact constant says the code now does otherwise.           *)
(*                                                                         *)
(* File references are to the repo root. "godbus" is                      *)
(* github.com/godbus/dbus/v5 at the version in go.mod.                     *)
(***************************************************************************)
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS
  MaxItems,  \* ids Menu.newItem hands out
  MaxOps,    \* app calls into the Menu and MenuItem API
  MaxLabel,  \* label values per item
  \* Code facts. scripts/check-menu-sync reads each one from the code.
  LateReply,               \* GetLayout and GetGroupProperties leave their reply to godbus,
                           \* which writes it after the deferred Menu.m unlock
  RevisionBumps,           \* updateLayout: menu.revision++ before its Emit loop
  AddChildSignalsLayout,   \* Menu.AddChild and MenuItem.AddChild call updateLayout
  RemoveSignalsLayout,     \* MenuItem.Remove calls updateLayout
  AppendSignalsLayout,     \* appendChild calls updateLayout
  MoveBeforeSignalsLayout, \* MoveBefore calls updateLayout
  SetPropsSignalsProps,    \* MenuItem.SetProps calls emitPropertiesUpdated
  SetPropsSignalsLayout,   \* MenuItem.SetProps calls updateLayout
  RemoveLeavesOrphans,     \* MenuItem.Remove deletes only that item from Menu.nodes
  RemovedKeepsParent,      \* getParent can return non-nil after Remove
  ParentMustBeLive,        \* AddChild/AppendChild refuse a receiver not in Menu.nodes
  RestoreRemovedToNodes,   \* AppendChild/MoveBefore put a removed child back in Menu.nodes
  AboutToShowGroupRejectsRoot  \* AboutToShowGroup treats id 0 as missing from Menu.nodes

Root == 0
Ids == 1..MaxItems
NodeIds == {Root} \cup Ids

VARIABLES
  nodes,    \* ids in Menu.nodes
  kids,     \* Menu.children and each MenuItem.children
  par,      \* MenuItem.parent
  label,    \* MenuItem.props["label"]
  rev,      \* Menu.revision
  lastId,   \* Menu.id
  ops,      \* app calls made so far
  out,      \* messages on the item's connection to the host, in order
  calls,    \* calls the host sent that the item has not served
  served,   \* replies built under Menu.m but not yet written
  hv,       \* the host's copy of the menu
  want,     \* the host's latest layout update; older ones are cancelled
  revLog,   \* revision of each layout change, starting with the initial one
  menuShut  \* a label change sent LayoutUpdated, which closes an open GNOME menu

vars == <<nodes, kids, par, label, rev, lastId, ops, out, calls, served,
          hv, want, revLog, menuShut>>

Range(s) == {s[i] : i \in DOMAIN s}

Remove(s, x) == SelectSeq(s, LAMBDA y : y # x)

InsertBefore(s, x, sib) ==
  LET i == CHOOSE j \in 1..(Len(s) + 1) :
             j = Len(s) + 1 \/ s[j] = sib
  IN SubSeq(s, 1, i - 1) \o <<x>> \o SubSeq(s, i, Len(s))

\* Ids the app still holds a *MenuItem pointer to, including removed ones.
Created == 1..lastId

\* Nodes reachable from n through children that are in Menu.nodes, as
\* dbusmenu.buildChildren walks them.
Below(k, n) ==
  LET R[i \in 0..MaxItems] ==
        IF i = 0 THEN {n}
        ELSE R[i - 1] \cup UNION {Range(k[m]) \cap nodes : m \in R[i - 1]}
  IN R[MaxItems]

\* children-display: setChildren stores "submenu" for an item with
\* children and "" without. buildLayout gives the root "submenu".
CD(k, n) == n = Root \/ Len(k[n]) > 0

\* What GetLayout(0, -1) returns: dbusmenu.buildLayout copies every
\* property with mapSlice(item.props, nil), whatever names were asked for.
View(k, l) == [n \in Below(k, Root) |->
                 [k |-> SelectSeq(k[n], LAMBDA c: c \in nodes),
                  l |-> l[n], c |-> CD(k, n)]]

\* MenuItem.getParent is non-nil. When RemovedKeepsParent is FALSE, the
\* code also treats an item missing from Menu.nodes as having no parent.
HasParent(n) ==
  /\ RemovedKeepsParent \/ n \in nodes
  /\ par[n] = Root \/ par[n] \in nodes

\* A parent the AddChild and AppendChild receivers accept.
LiveDest(p) == p = Root \/ (p \in Created /\ (~ParentMustBeLive \/ p \in nodes))

\* ids AboutToShowGroup reports in invalid. Root is never in Menu.nodes.
GroupInvalid(ids) ==
  IF AboutToShowGroupRejectsRoot
  THEN {id \in Range(ids) : id \notin nodes}
  ELSE {id \in Range(ids) : id # Root /\ id \notin nodes}

LayoutMsg(r, p) == [t |-> "layout", rev |-> r, parent |-> p]
LabelMsg(n, v) == [t |-> "props", id |-> n, f |-> "l", v |-> v]
CDMsg(n, v) == [t |-> "props", id |-> n, f |-> "c", v |-> v]

\* updateLayout(ps): one revision, then one LayoutUpdated per node.
NewRev == IF RevisionBumps THEN rev + 1 ELSE rev
Layout(ps) == [i \in 1..Len(ps) |-> LayoutMsg(NewRev, ps[i])]
Relayout(on) ==
  IF on
  THEN /\ rev' = NewRev
       /\ revLog' = Append(revLog, NewRev)
  ELSE UNCHANGED <<rev, revLog>>
LayoutIf(on, ps) == IF on THEN Layout(ps) ELSE <<>>

\* emitPropertiesUpdated for a children-display change from setChildren.
CDIf(n, before, after) ==
  IF n # Root /\ CD(before, n) # CD(after, n) THEN <<CDMsg(n, CD(after, n))>>
  ELSE <<>>

Init ==
  /\ nodes = {}
  /\ kids = [n \in NodeIds |-> <<>>]
  /\ par = [n \in Ids |-> Root]
  /\ label = [n \in NodeIds |-> 0]
  /\ rev = 0
  /\ lastId = 0
  /\ ops = 0
  /\ out = <<>>
  /\ calls = {}
  /\ served = {}
  /\ hv = View(kids, label)
  /\ want = 0
  /\ revLog = <<0>>
  /\ menuShut = FALSE

-----------------------------------------------------------------------------
(* The app. Each action is one Menu or MenuItem method, menuitem.go.        *)

\* Menu.AddChild(MenuItemLabel(..)) for p = Root, MenuItem.AddChild for an
\* item p, including a held pointer to a removed item unless
\* ParentMustBeLive. newItem, applyProps, setChildren, updateLayout(p),
\* the child's emitPropertiesUpdated, then the parent's children-display.
AddChild(p) ==
  LET c == lastId + 1
      k2 == [kids EXCEPT ![p] = Append(@, c)]
  IN /\ lastId < MaxItems
     /\ LiveDest(p)
     /\ nodes' = nodes \cup {c}
     /\ par' = [par EXCEPT ![c] = p]
     /\ kids' = k2
     /\ label' = [label EXCEPT ![c] = 1]
     /\ lastId' = c
     /\ Relayout(AddChildSignalsLayout)
     /\ out' = out \o LayoutIf(AddChildSignalsLayout, <<p>>)
                   \o <<LabelMsg(c, 1)>> \o CDIf(p, kids, k2)
     /\ UNCHANGED menuShut

\* MenuItem.SetProps(MenuItemLabel(..)) with a new label. SetProps
\* returns an error when getParent is nil, including after Remove once
\* RemovedKeepsParent is FALSE. A held pointer to a removed root child
\* still has parent 0, so the current code applies the change.
SetLabel(n) ==
  /\ n \in Created /\ HasParent(n)
  /\ label[n] < MaxLabel
  /\ label' = [label EXCEPT ![n] = @ + 1]
  /\ Relayout(SetPropsSignalsLayout)
  /\ out' = out \o (IF SetPropsSignalsProps THEN <<LabelMsg(n, label[n] + 1)>> ELSE <<>>)
                \o LayoutIf(SetPropsSignalsLayout, <<par[n]>>)
  /\ menuShut' = (menuShut \/ SetPropsSignalsLayout)
  /\ UNCHANGED <<nodes, kids, par, lastId>>

\* MenuItem.Remove. When RemoveLeavesOrphans, only n is deleted from
\* Menu.nodes and its descendants stay. item.parent is left as it is
\* unless RemovedKeepsParent is FALSE.
RemoveItem(n) ==
  LET p == par[n]
      k2 == [kids EXCEPT ![p] = Remove(@, n)]
      drop == IF RemoveLeavesOrphans THEN {n} ELSE Below(kids, n)
  IN /\ n \in Created /\ HasParent(n)
     /\ nodes' = nodes \ drop
     /\ kids' = k2
     /\ par' = IF RemovedKeepsParent THEN par
               ELSE [i \in Ids |-> IF i \in drop THEN Root ELSE par[i]]
     \* par' = Root is only used when RemovedKeepsParent is FALSE, and
     \* HasParent then requires n \in nodes, so getParent is still nil.
     /\ Relayout(RemoveSignalsLayout)
     /\ out' = out \o LayoutIf(RemoveSignalsLayout, <<p>>) \o CDIf(p, kids, k2)
     /\ UNCHANGED <<label, lastId, menuShut>>

\* appendChild (Menu.AppendChild, MenuItem.AppendChild) when sib = 0, and
\* MoveBefore(sib) otherwise. dst never sits under c: the library does not
\* check that, and a cycle makes buildLayout recurse forever. A held
\* pointer to a removed child is accepted while getParent is non-nil. The
\* current code then inserts the id into dst's children and does not put
\* it back into Menu.nodes unless RestoreRemovedToNodes.
Move(c, dst, sib, signals) ==
  LET src == par[c]
      k1 == [kids EXCEPT ![src] = Remove(@, c)]
      k2 == [k1 EXCEPT ![dst] = IF sib = 0 THEN Append(@, c)
                                ELSE InsertBefore(@, c, sib)]
      ups == IF dst = src THEN <<dst>> ELSE <<dst, src>>
  IN /\ c \in Created /\ HasParent(c)
     /\ LiveDest(dst)
     /\ dst \notin Below(kids, c)
     /\ kids' = k2
     /\ par' = [par EXCEPT ![c] = dst]
     /\ nodes' = IF RestoreRemovedToNodes THEN nodes \cup {c} ELSE nodes
     /\ Relayout(signals)
     /\ out' = out \o LayoutIf(signals, ups)
                   \o CDIf(src, kids, k1) \o CDIf(dst, k1, k2)
     /\ UNCHANGED <<label, lastId, menuShut>>

AppendChild(c, dst) == Move(c, dst, 0, AppendSignalsLayout)

MoveBefore(c, sib) == /\ sib \in Created /\ sib # c /\ HasParent(sib)
                      /\ Move(c, par[sib], sib, MoveBeforeSignalsLayout)

\* One app call. Each is its own definition so TLC names the step.
AppCall == /\ ops < MaxOps
           /\ ops' = ops + 1
           /\ UNCHANGED <<calls, served, hv, want>>

AppAddsItem == AppCall /\ \E p \in {Root} \cup Created : AddChild(p)
AppSetsLabel == AppCall /\ \E n \in Created : SetLabel(n)
AppRemovesItem == AppCall /\ \E n \in Created : RemoveItem(n)
AppAppendsChild == AppCall /\ \E c \in Created, d \in {Root} \cup Created : AppendChild(c, d)
AppMovesBefore == AppCall /\ \E c, s \in Created : MoveBefore(c, s)

-----------------------------------------------------------------------------
(* The item's side of GetLayout and GetGroupProperties, dbusmenu.go and    *)
(* godbus.                                                                  *)

\* godbus runs each call on its own goroutine. GetLayout builds its reply
\* under Menu.m. GetGroupProperties reads every id in the call that is in
\* Menu.nodes under Menu.m. Without LateReply, the method writes its reply
\* with conn.Send before it unlocks, so building and writing are one step.
\* With LateReply, godbus writes the reply as a later step.
ReplyTo(c) ==
  IF c.kind = "layout"
  THEN [t |-> "layout-reply", gen |-> c.gen, view |-> View(kids, label)]
  ELSE [t |-> "props-reply", gen |-> c.gen,
        vals |-> [n \in c.ids \cap nodes |-> [l |-> label[n], c |-> CD(kids, n)]]]

Serve(c) ==
  /\ calls' = calls \ {c}
  /\ IF LateReply
     THEN /\ served' = served \cup {ReplyTo(c)}
          /\ UNCHANGED out
     ELSE /\ out' = Append(out, ReplyTo(c))
          /\ UNCHANGED served
  /\ UNCHANGED <<nodes, kids, par, label, rev, lastId, ops, hv, want,
                 revLog, menuShut>>

Reply(r) ==
  /\ served' = served \ {r}
  /\ out' = Append(out, r)
  /\ UNCHANGED <<nodes, kids, par, label, rev, lastId, ops, calls, hv, want,
                 revLog, menuShut>>

-----------------------------------------------------------------------------
(* The host: DBusClient._onSignal, _onPropertiesUpdated,                   *)
(* _layoutUpdateUpdateAsync, _doLayoutUpdate, and _requestProperties in     *)
(* the GNOME extension's dbusMenu.js. A new layout update cancels the       *)
(* previous one and the GetGroupProperties call it started.                 *)

Host ==
  LET m == Head(out) IN
  /\ out # <<>>
  /\ out' = Tail(out)
  /\ CASE m.t = "layout" ->
            /\ want' = want + 1
            /\ calls' = calls \cup {[kind |-> "layout", gen |-> want + 1]}
            /\ UNCHANGED hv
       [] m.t = "props" ->
            /\ hv' = IF m.id \in DOMAIN hv
                     THEN [hv EXCEPT ![m.id][m.f] = m.v] ELSE hv
            /\ UNCHANGED <<want, calls>>
       [] m.t = "layout-reply" ->
            LET new == DOMAIN m.view \ DOMAIN hv IN
            /\ hv' = IF m.gen = want THEN m.view ELSE hv
            /\ calls' = IF m.gen = want /\ new # {}
                        THEN calls \cup {[kind |-> "props", gen |-> want, ids |-> new]}
                        ELSE calls
            /\ UNCHANGED want
       [] m.t = "props-reply" ->
            /\ hv' = IF m.gen = want
                     THEN [n \in DOMAIN hv |->
                             IF n \in DOMAIN m.vals
                             THEN [hv[n] EXCEPT !.l = m.vals[n].l, !.c = m.vals[n].c]
                             ELSE hv[n]]
                     ELSE hv
            /\ UNCHANGED <<want, calls>>
  /\ UNCHANGED <<nodes, kids, par, label, rev, lastId, ops, served, revLog,
                 menuShut>>

ItemServes == \E c \in calls : Serve(c)
ReplyWritten == \E r \in served : Reply(r)

\* End state: no call is in flight, and no further app call is possible.
Done ==
  /\ out = <<>> /\ calls = {} /\ served = {}
  /\ ops = MaxOps
  /\ UNCHANGED vars

Next ==
  \/ AppAddsItem
  \/ AppSetsLabel
  \/ AppRemovesItem
  \/ AppAppendsChild
  \/ AppMovesBefore
  \/ ItemServes
  \/ ReplyWritten
  \/ Host
  \/ Done

Spec == Init /\ [][Next]_vars

-----------------------------------------------------------------------------
(* Properties.                                                              *)

TypeOK ==
  /\ nodes \subseteq Ids
  /\ rev \in Nat
  /\ want \in Nat
  /\ menuShut \in BOOLEAN

\* Nothing is on its way between the item and the host.
Settled == out = <<>> /\ calls = {} /\ served = {}

Shown == View(kids, label)

\* Once settled, the host shows the same items in the same order.
LayoutConverges ==
  Settled => [n \in DOMAIN hv |-> hv[n].k] = [n \in DOMAIN Shown |-> Shown[n].k]

\* Once settled, every item the host shows has the app's label and
\* children-display.
PropsConverge ==
  Settled => \A n \in DOMAIN hv \cap DOMAIN Shown :
               hv[n].l = Shown[n].l /\ hv[n].c = Shown[n].c

\* Every layout change carries a revision newer than every earlier one.
RevisionsIncrease ==
  \A i \in 1..(Len(revLog) - 1) : revLog[i] < revLog[i + 1]

\* A label change never sends LayoutUpdated.
LabelChangeKeepsMenuOpen == ~menuShut

\* Every id in Menu.nodes is reachable from the root through live children.
NodesReachable == nodes \subseteq Below(kids, Root)

\* Every id in a live children list is in Menu.nodes.
NoDanglingChildIDs ==
  \A n \in {Root} \cup nodes : Range(kids[n]) \subseteq nodes

\* A removed item has getParent() == nil.
RemovedHasNoParent == \A n \in Created \ nodes : ~HasParent(n)

\* AboutToShowGroup([0]) checks the root like AboutToShow(0), and does
\* not report 0 as invalid.
AboutToShowGroupAcceptsRoot == Root \notin GroupInvalid(<<Root>>)

=============================================================================
