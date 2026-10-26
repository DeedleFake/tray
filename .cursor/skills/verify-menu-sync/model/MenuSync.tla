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
(* godbus writes a reply after the method unlocks Menu.m, so a property    *)
(* change can send its signal ahead of a reply that holds the old value.   *)
(* The item tracks those replies (pendingReplies in replies.go) and sends  *)
(* the changed items' properties again once godbus has written the reply. *)
(*                                                                         *)
(* File references are to the repo root. "godbus" is                      *)
(* github.com/godbus/dbus/v5 at the version in go.mod.                     *)
(***************************************************************************)
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS
  MaxItems,  \* ids Menu.newItem hands out
  MaxOps,    \* app calls into the Menu and MenuItem API
  MaxLabel,  \* label values per item
  \* Bound: godbus handleCall (export.go) writes a method's reply after
  \* the method returns, so after its deferred Menu.m unlock.
  LateReply,
  \* Code facts. scripts/check-menu-sync reads each one from the code.
  RevisionBumps,           \* updateLayout: menu.revision++ before its Emit loop
  AddChildSignalsLayout,   \* Menu.AddChild and MenuItem.AddChild call updateLayout
  RemoveSignalsLayout,     \* MenuItem.Remove calls updateLayout
  AppendSignalsLayout,     \* appendChild calls updateLayout
  MoveBeforeSignalsLayout, \* MoveBefore calls updateLayout
  SetPropsSignalsProps,    \* MenuItem.SetProps calls emitPropertiesUpdated
  SetPropsSignalsLayout,   \* MenuItem.SetProps calls updateLayout
  ResendAfterReply         \* pendingReplies is wired in and resends after a reply

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
  served,   \* replies built under Menu.m but not yet written, with the
            \* items whose properties changed since the reply was built
  resend,   \* sets of items replyWritten has yet to send again
  hv,       \* the host's copy of the menu
  want,     \* the host's latest layout update; older ones are cancelled
  revLog,   \* revision of each layout change, starting with the initial one
  menuShut  \* a label change sent LayoutUpdated, which closes an open GNOME menu

vars == <<nodes, kids, par, label, rev, lastId, ops, out, calls, served,
          resend, hv, want, revLog, menuShut>>

Range(s) == {s[i] : i \in DOMAIN s}

Remove(s, x) == SelectSeq(s, LAMBDA y : y # x)

InsertBefore(s, x, sib) ==
  LET i == CHOOSE j \in 1..(Len(s) + 1) :
             j = Len(s) + 1 \/ s[j] = sib
  IN SubSeq(s, 1, i - 1) \o <<x>> \o SubSeq(s, i, Len(s))

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
                 [k |-> k[n], l |-> l[n], c |-> CD(k, n)]]

\* A parent the methods accept: MenuItem.getParent is non-nil.
HasParent(n) == par[n] = Root \/ par[n] \in nodes

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
  /\ resend = {}
  /\ hv = View(kids, label)
  /\ want = 0
  /\ revLog = <<0>>
  /\ menuShut = FALSE

-----------------------------------------------------------------------------
(* The app. Each action is one Menu or MenuItem method, menuitem.go.        *)

\* Menu.AddChild(MenuItemLabel(..)) for p = Root, MenuItem.AddChild for an
\* item p: newItem, applyProps, setChildren, updateLayout(p), the child's
\* emitPropertiesUpdated, then the parent's children-display.
AddChild(p) ==
  LET c == lastId + 1
      k2 == [kids EXCEPT ![p] = Append(@, c)]
  IN /\ lastId < MaxItems
     /\ nodes' = nodes \cup {c}
     /\ par' = [par EXCEPT ![c] = p]
     /\ kids' = k2
     /\ label' = [label EXCEPT ![c] = 1]
     /\ lastId' = c
     /\ Relayout(AddChildSignalsLayout)
     /\ out' = out \o LayoutIf(AddChildSignalsLayout, <<p>>)
                   \o <<LabelMsg(c, 1)>> \o CDIf(p, kids, k2)
     /\ UNCHANGED menuShut

\* MenuItem.SetProps(MenuItemLabel(..)) with a new label.
SetLabel(n) ==
  /\ n \in nodes /\ HasParent(n)
  /\ label[n] < MaxLabel
  /\ label' = [label EXCEPT ![n] = @ + 1]
  /\ Relayout(SetPropsSignalsLayout)
  /\ out' = out \o (IF SetPropsSignalsProps THEN <<LabelMsg(n, label[n] + 1)>> ELSE <<>>)
                \o LayoutIf(SetPropsSignalsLayout, <<par[n]>>)
  /\ menuShut' = (menuShut \/ SetPropsSignalsLayout)
  /\ UNCHANGED <<nodes, kids, par, lastId>>

\* MenuItem.Remove.
RemoveItem(n) ==
  LET p == par[n]
      k2 == [kids EXCEPT ![p] = Remove(@, n)]
  IN /\ n \in nodes /\ HasParent(n)
     /\ nodes' = nodes \ {n}
     /\ kids' = k2
     /\ Relayout(RemoveSignalsLayout)
     /\ out' = out \o LayoutIf(RemoveSignalsLayout, <<p>>) \o CDIf(p, kids, k2)
     /\ UNCHANGED <<par, label, lastId, menuShut>>

\* appendChild (Menu.AppendChild, MenuItem.AppendChild) when sib = 0, and
\* MoveBefore(sib) otherwise. dst never sits under c: the library does not
\* check that, and a cycle makes buildLayout recurse forever.
Move(c, dst, sib, signals) ==
  LET src == par[c]
      k1 == [kids EXCEPT ![src] = Remove(@, c)]
      k2 == [k1 EXCEPT ![dst] = IF sib = 0 THEN Append(@, c)
                                ELSE InsertBefore(@, c, sib)]
      ups == IF dst = src THEN <<dst>> ELSE <<dst, src>>
  IN /\ c \in nodes /\ HasParent(c)
     /\ dst \notin Below(kids, c)
     /\ kids' = k2
     /\ par' = [par EXCEPT ![c] = dst]
     /\ Relayout(signals)
     /\ out' = out \o LayoutIf(signals, ups)
                   \o CDIf(src, kids, k1) \o CDIf(dst, k1, k2)
     /\ UNCHANGED <<nodes, label, lastId, menuShut>>

AppendChild(c, dst) == /\ dst = Root \/ dst \in nodes
                       /\ Move(c, dst, 0, AppendSignalsLayout)

MoveBefore(c, sib) == /\ sib \in nodes /\ sib # c /\ HasParent(sib)
                      /\ Move(c, par[sib], sib, MoveBeforeSignalsLayout)

\* emitPropertiesUpdated calls pendingReplies.changed for its item, so
\* every unwritten reply records the items the method sent
\* ItemsPropertiesUpdated for. The code starts recording when the call
\* arrives, before the method reads the menu. A change before the read
\* is in the reply anyway, so the model starts at Serve.
Changed == {out'[i].id : i \in {j \in (Len(out) + 1)..Len(out') :
                                  out'[j].t = "props"}}
Track(rs) == {[r EXCEPT !.chg = @ \cup Changed] : r \in rs}

\* One app call. Each is its own definition so TLC names the step. It
\* comes after the method so that out' is known.
AppCall == /\ ops < MaxOps
           /\ ops' = ops + 1
           /\ served' = Track(served)
           /\ UNCHANGED <<calls, resend, hv, want>>

AppAddsItem == (\E p \in {Root} \cup nodes : AddChild(p)) /\ AppCall
AppSetsLabel == (\E n \in nodes : SetLabel(n)) /\ AppCall
AppRemovesItem == (\E n \in nodes : RemoveItem(n)) /\ AppCall
AppAppendsChild == (\E c \in nodes, d \in {Root} \cup nodes : AppendChild(c, d)) /\ AppCall
AppMovesBefore == (\E c, s \in nodes : MoveBefore(c, s)) /\ AppCall

-----------------------------------------------------------------------------
(* The item's side of GetLayout and GetGroupProperties, dbusmenu.go and    *)
(* godbus.                                                                  *)

\* godbus runs each call on its own goroutine. GetLayout builds its reply
\* under Menu.m. GetGroupProperties reads every id in the call that is in
\* Menu.nodes under Menu.m. With LateReply, godbus writes the reply as a
\* later step.
ReplyTo(c) ==
  IF c.kind = "layout"
  THEN [t |-> "layout-reply", gen |-> c.gen, view |-> View(kids, label)]
  ELSE [t |-> "props-reply", gen |-> c.gen,
        vals |-> [n \in c.ids \cap nodes |-> [l |-> label[n], c |-> CD(kids, n)]]]

Serve(c) ==
  /\ calls' = calls \ {c}
  /\ IF LateReply
     THEN /\ served' = served \cup {[msg |-> ReplyTo(c), chg |-> {}]}
          /\ UNCHANGED out
     ELSE /\ out' = Append(out, ReplyTo(c))
          /\ UNCHANGED served
  /\ UNCHANGED <<nodes, kids, par, label, rev, lastId, ops, resend, hv, want,
                 revLog, menuShut>>

\* godbus writes the reply, then calls serialGenerator.RetireSerial with
\* its serial, which hands the items that changed to replyWritten.
Reply(r) ==
  /\ served' = served \ {r}
  /\ out' = Append(out, r.msg)
  /\ resend' = IF ResendAfterReply /\ r.chg # {} THEN resend \cup {r.chg}
               ELSE resend
  /\ UNCHANGED <<nodes, kids, par, label, rev, lastId, ops, calls, hv, want,
                 revLog, menuShut>>

\* replyWritten: under Menu.m, one ItemsPropertiesUpdated with every
\* current property of each item still in Menu.nodes. It sends them with
\* sendPropertiesUpdated, which does not call pendingReplies.changed.
Resend(ids) ==
  LET live == ids \cap nodes IN
  /\ resend' = resend \ {ids}
  /\ out' = IF live = {} THEN out
            ELSE Append(out, [t |-> "resend",
                              vals |-> [n \in live |-> [l |-> label[n], c |-> CD(kids, n)]]])
  /\ UNCHANGED <<nodes, kids, par, label, rev, lastId, ops, calls, served,
                 hv, want, revLog, menuShut>>

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
       [] m.t = "resend" ->
            /\ hv' = [n \in DOMAIN hv |->
                       IF n \in DOMAIN m.vals
                       THEN [hv[n] EXCEPT !.l = m.vals[n].l, !.c = m.vals[n].c]
                       ELSE hv[n]]
            /\ UNCHANGED <<want, calls>>
  /\ UNCHANGED <<nodes, kids, par, label, rev, lastId, ops, served, resend,
                 revLog, menuShut>>

ItemServes == \E c \in calls : Serve(c)
ReplyWritten == \E r \in served : Reply(r)
ItemResends == \E ids \in resend : Resend(ids)

Next ==
  \/ AppAddsItem
  \/ AppSetsLabel
  \/ AppRemovesItem
  \/ AppAppendsChild
  \/ AppMovesBefore
  \/ ItemServes
  \/ ReplyWritten
  \/ ItemResends
  \/ Host

Spec == Init /\ [][Next]_vars

-----------------------------------------------------------------------------
(* Properties.                                                              *)

TypeOK ==
  /\ nodes \subseteq Ids
  /\ rev \in Nat
  /\ want \in Nat
  /\ menuShut \in BOOLEAN

\* Nothing is on its way between the item and the host.
Settled == out = <<>> /\ calls = {} /\ served = {} /\ resend = {}

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

=============================================================================
