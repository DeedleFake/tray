------------------------------ MODULE MenuLocks ------------------------------
(***************************************************************************)
(* Every lock that deedles.dev/tray's menu code takes, as Go's             *)
(* sync.RWMutex does it, with app goroutines calling the Menu and MenuItem *)
(* API while godbus runs host calls on goroutines of their own. Each       *)
(* goroutine runs a program of single lock steps built from the function   *)
(* it is in, so a goroutine can stop between any two of them.              *)
(*                                                                         *)
(* Mutexes: 0 is Menu.m and an item id is that MenuItem.m.                 *)
(*                                                                         *)
(* Two kinds of code are not steps. conn.Emit, conn.Send, and godbus       *)
(* writing a reply take only godbus's own send locks (conn.go              *)
(* sendAndIfClosed), which guard nothing else and are released after the   *)
(* write, so they change nothing another goroutine can wait on. A          *)
(* function's reads and writes of the menu tree happen under Menu.m, which *)
(* every other reader of the tree needs, so they happen at the step that   *)
(* took Menu.m.                                                            *)
(*                                                                         *)
(* File references are to the repo root. "godbus" is                       *)
(* github.com/godbus/dbus/v5 at the version in go.mod.                     *)
(***************************************************************************)
EXTENDS Integers, Sequences, FiniteSets, TLC

CONSTANTS
  InitItems,  \* items the menu already has, in every tree shape
  MaxItems,   \* ids Menu.newItem hands out, so AddChild calls add the rest
  MaxOps,     \* app calls into the API, over all app goroutines
  MaxCalls,   \* host method calls, each on its own godbus goroutine
  AppProcs,   \* app goroutines
  CallProcs,  \* godbus goroutines serving host calls
  \* Code shapes. scripts/check-menu-locks reads each one from the code.
  ItemFirst,          \* methods that lock their items before Menu.m
  EventHoldsMenuRead, \* dbusmenu.event holds Menu.m for reading around the handler
  ShowGroupCallsShow  \* AboutToShowGroup calls AboutToShow under its own read lock

Root == 0
Ids == 1..MaxItems
Mutexes == {Root} \cup Ids
Procs == AppProcs \cup CallProcs
None == "none"

VARIABLES
  prog,   \* each goroutine's remaining steps
  wr,     \* the goroutine holding a mutex's writer slot (RWMutex.w), or None
  wheld,  \* that writer got the lock, after the readers it waited for left
  rd,     \* read locks each goroutine holds on each mutex
  rwait,  \* goroutines blocked in RLock behind a waiting or holding writer
  nodes,  \* ids in Menu.nodes
  par,    \* MenuItem.parent
  lastId, \* Menu.id
  ops,    \* app calls started
  calls   \* host calls started

vars == <<prog, wr, wheld, rd, rwait, nodes, par, lastId, ops, calls>>

\* A step. at names the code it comes from.
S(op, m, at) == [op |-> op, m |-> m, k |-> "", a |-> 0, b |-> 0, at |-> at]
\* Where a function reads the menu tree, always right after it takes
\* Menu.m. The step that takes Menu.m expands it into the rest of the
\* function and makes the function's change to the tree.
Plan(k, a, b, at) == [op |-> "plan", m |-> Root, k |-> k, a |-> a, b |-> b, at |-> at]

Lock(m, at) == <<S("Lock", m, at)>>
Unlock(m, at) == <<S("Unlock", m, at)>>
RLock(m, at) == <<S("RLock", m, at)>>
RUnlock(m, at) == <<S("RUnlock", m, at)>>
\* conn.Emit, a menu method's conn.Send of its own reply, and godbus's
\* reply. Not steps, see the top.
Emit(at) == <<>>
Send(at) == <<>>
Reply == <<>>

RECURSIVE Cat(_)
Cat(s) == IF s = <<>> THEN <<>> ELSE Head(s) \o Cat(Tail(s))

\* Ids of a set in increasing order.
RECURSIVE Sorted(_)
Sorted(X) == IF X = {} THEN <<>>
             ELSE LET x == CHOOSE y \in X : \A z \in X : y <= z
                  IN <<x>> \o Sorted(X \ {x})

\* An item's children in Menu.nodes. buildChildren walks item.children in
\* slice order, and Walk goes in id order. GetLayout holds Menu.m for
\* writing, so no writer can be waiting on an item lock, and the order of
\* its read locks cannot matter.
Kids(n) == {c \in nodes : par[c] = n}

\* MenuItem.getParent: Root, a live item, or nil (Gone) when the parent
\* was removed.
Gone == -1
Parent(n) == IF par[n] = Root THEN Root
             ELSE IF par[n] \in nodes THEN par[n] ELSE Gone

\* Items at or below n, so a move into one of them would make a cycle.
Below(n) == LET R[i \in 0..MaxItems] ==
                  IF i = 0 THEN {n}
                  ELSE R[i - 1] \cup {c \in nodes : par[c] \in R[i - 1]}
            IN R[MaxItems]

-----------------------------------------------------------------------------
(* App calls, menuitem.go and menu.go. Deferred unlocks run in reverse.     *)

\* MenuItem.SetProps: lock Menu.m and the item, getParent,
\* emitPropertiesUpdated.
SetPropsHead(x) ==
  IF "SetProps" \in ItemFirst
  THEN Lock(x, "menuitem.go MenuItem.SetProps: defer item.lock()()")
       \o Lock(Root, "menuitem.go MenuItem.SetProps: defer item.menu.lock()()")
       \o <<Plan("SetProps", x, 0, "menuitem.go MenuItem.SetProps: item.getParent()")>>
  ELSE Lock(Root, "menuitem.go MenuItem.SetProps: defer item.menu.lock()()")
       \o Lock(x, "menuitem.go MenuItem.SetProps: defer item.lock()()")
       \o <<Plan("SetProps", x, 0, "menuitem.go MenuItem.SetProps: item.getParent()")>>

SetPropsTail(x) ==
  LET unlocks == IF "SetProps" \in ItemFirst
                 THEN Unlock(Root, "menuitem.go MenuItem.SetProps: deferred menu unlock")
                      \o Unlock(x, "menuitem.go MenuItem.SetProps: deferred item unlock")
                 ELSE Unlock(x, "menuitem.go MenuItem.SetProps: deferred item unlock")
                      \o Unlock(Root, "menuitem.go MenuItem.SetProps: deferred menu unlock")
  IN (IF Parent(x) = Gone THEN <<>>
      ELSE Emit("menuitem.go MenuItem.emitPropertiesUpdated: conn.Emit ItemsPropertiesUpdated"))
     \o unlocks

\* Menu.AddChild: lock Menu.m, newItem, lock the child, updateLayout,
\* the child's emitPropertiesUpdated, setChildren.
AddRootHead == Lock(Root, "menuitem.go Menu.AddChild: defer menu.lock()()")
               \o <<Plan("AddRoot", 0, 0, "menuitem.go Menu.AddChild: menu.newItem(0)")>>

AddRootTail(c) ==
  Lock(c, "menuitem.go Menu.AddChild: defer child.lock()()")
  \o Emit("menu.go Menu.updateLayout: conn.Emit LayoutUpdated")
  \o Emit("menuitem.go MenuItem.emitPropertiesUpdated: conn.Emit ItemsPropertiesUpdated")
  \o Unlock(c, "menuitem.go Menu.AddChild: deferred child unlock")
  \o Unlock(Root, "menuitem.go Menu.AddChild: deferred menu unlock")

\* MenuItem.AddChild: lock Menu.m, the item, newItem, the child,
\* setChildren, updateLayout, the child's and then the item's
\* emitPropertiesUpdated.
AddHead(x) ==
  IF "AddChild" \in ItemFirst
  THEN Lock(x, "menuitem.go MenuItem.AddChild: defer item.lock()()")
       \o Lock(Root, "menuitem.go MenuItem.AddChild: defer item.menu.lock()()")
       \o <<Plan("Add", x, 0, "menuitem.go MenuItem.AddChild: item.menu.newItem(item.id)")>>
  ELSE Lock(Root, "menuitem.go MenuItem.AddChild: defer item.menu.lock()()")
       \o Lock(x, "menuitem.go MenuItem.AddChild: defer item.lock()()")
       \o <<Plan("Add", x, 0, "menuitem.go MenuItem.AddChild: item.menu.newItem(item.id)")>>

AddTail(x, c) ==
  Lock(c, "menuitem.go MenuItem.AddChild: defer child.lock()()")
  \o Emit("menu.go Menu.updateLayout: conn.Emit LayoutUpdated")
  \o Emit("menuitem.go MenuItem.emitPropertiesUpdated: conn.Emit ItemsPropertiesUpdated")
  \o (IF Kids(x) = {}
      THEN Emit("menuitem.go MenuItem.AddChild: parent emitPropertiesUpdated(children-display)")
      ELSE <<>>)
  \o Unlock(c, "menuitem.go MenuItem.AddChild: deferred child unlock")
  \o (IF "AddChild" \in ItemFirst
      THEN Unlock(Root, "menuitem.go MenuItem.AddChild: deferred menu unlock")
           \o Unlock(x, "menuitem.go MenuItem.AddChild: deferred item unlock")
      ELSE Unlock(x, "menuitem.go MenuItem.AddChild: deferred item unlock")
           \o Unlock(Root, "menuitem.go MenuItem.AddChild: deferred menu unlock"))

\* MenuItem.Remove: lock Menu.m, getParent, lock an item parent,
\* setChildren, delete from Menu.nodes, updateLayout, the parent's
\* emitPropertiesUpdated.
RemoveHead(x) == Lock(Root, "menuitem.go MenuItem.Remove: defer item.menu.lock()()")
                 \o <<Plan("Remove", x, 0, "menuitem.go MenuItem.Remove: item.getParent()")>>

RemoveTail(x) ==
  LET p == Parent(x) IN
  IF p = Gone THEN Unlock(Root, "menuitem.go MenuItem.Remove: return nil, deferred menu unlock")
  ELSE (IF p # Root THEN Lock(p, "menuitem.go MenuItem.Remove: defer parent.lock()()") ELSE <<>>)
       \o Emit("menu.go Menu.updateLayout: conn.Emit LayoutUpdated")
       \o (IF p # Root /\ Kids(p) = {x}
           THEN Emit("menuitem.go MenuItem.Remove: p.emitPropertiesUpdated(children-display)")
           ELSE <<>>)
       \o (IF p # Root THEN Unlock(p, "menuitem.go MenuItem.Remove: deferred parent unlock") ELSE <<>>)
       \o Unlock(Root, "menuitem.go MenuItem.Remove: deferred menu unlock")

\* appendChild (behind Menu.AppendChild and MenuItem.AppendChild) when
\* sib = 0, and MenuItem.MoveBefore(sib) otherwise: lock Menu.m, getParent,
\* lock dst unless it is the menu, lock the old parent unless it is dst or
\* the menu, move, updateLayout, then emitPropertiesUpdated for each
\* parent whose children-display changed.
MoveHead(c, dst, sib) ==
  IF sib = 0
  THEN Lock(Root, "menuitem.go appendChild: defer menu.lock()()")
       \o <<Plan("Append", c, dst, "menuitem.go appendChild: child.getParent()")>>
  ELSE Lock(Root, "menuitem.go MenuItem.MoveBefore: defer item.menu.lock()()")
       \o <<Plan("MoveBefore", c, sib, "menuitem.go MenuItem.MoveBefore: sibling.getParent(), item.getParent()")>>

MoveTail(c, dst, src, fn) ==
  IF dst = Gone \/ src = Gone
  THEN Unlock(Root, "menuitem.go " \o fn \o ": return nil, deferred menu unlock")
  ELSE LET lockDst == dst # Root
           lockSrc == src # dst /\ src # Root
       IN (IF lockDst THEN Lock(dst, "menuitem.go " \o fn \o ": defer dst.lock()()") ELSE <<>>)
          \o (IF lockSrc THEN Lock(src, "menuitem.go " \o fn \o ": defer parent.lock()() / src.lock()()") ELSE <<>>)
          \o Emit("menu.go Menu.updateLayout: conn.Emit LayoutUpdated")
          \o (IF src # dst THEN Emit("menu.go Menu.updateLayout: conn.Emit LayoutUpdated") ELSE <<>>)
          \o (IF src # Root /\ src # dst /\ Kids(src) = {c}
              THEN Emit("menuitem.go " \o fn \o ": emitPropertiesUpdated(children-display) for the old parent")
              ELSE <<>>)
          \o (IF dst # Root /\ src # dst /\ Kids(dst) = {}
              THEN Emit("menuitem.go " \o fn \o ": emitPropertiesUpdated(children-display) for dst")
              ELSE <<>>)
          \o (IF lockSrc THEN Unlock(src, "menuitem.go " \o fn \o ": deferred parent / src unlock") ELSE <<>>)
          \o (IF lockDst THEN Unlock(dst, "menuitem.go " \o fn \o ": deferred dst unlock") ELSE <<>>)
          \o Unlock(Root, "menuitem.go " \o fn \o ": deferred menu unlock")

\* MenuItem.Label, Type, Enabled, and the other getters.
Getter(x) == RLock(x, "menuitem.go MenuItem.Label and the other getters: item.m.RLock()")
             \o RUnlock(x, "menuitem.go MenuItem.Label and the other getters: deferred item.m.RUnlock()")

\* Item.SetProps(ItemMenuHandler(..)): the prop callback locks Menu.m.
MenuHandler == Lock(Root, "menu.go ItemMenuHandler: defer item.menu.lock()()")
               \o Unlock(Root, "menu.go ItemMenuHandler: deferred menu unlock")

-----------------------------------------------------------------------------
(* Host calls, dbusmenu.go. godbus handleCall runs each on its own         *)
(* goroutine. GetLayout, GetGroupProperties, and GetProperty write their   *)
(* reply with conn.Send before their deferred unlocks. For the others,     *)
(* godbus writes the reply after the method returns.                       *)

\* GetLayout(parentID, -1): Lock Menu.m, then buildLayout and
\* buildChildren read-lock each item while they walk its children, so an
\* item stays read-locked until its whole subtree is built.
RECURSIVE Walk(_, _)
Walk(n, d) ==
  IF d = 0 THEN <<>>
  ELSE LET inner == Cat([i \in 1..Cardinality(Kids(n)) |-> Walk(Sorted(Kids(n))[i], d - 1)])
       IN IF n = Root THEN inner
          ELSE RLock(n, "dbusmenu.go buildLayout: item.m.RLock()")
               \o inner
               \o RUnlock(n, "dbusmenu.go buildLayout: deferred item.m.RUnlock()")

GetLayoutHead(pid) == Lock(Root, "dbusmenu.go GetLayout: menu.m.Lock()")
                      \o <<Plan("GetLayout", pid, 0, "dbusmenu.go GetLayout: menu.nodes[parentID]")>>
GetLayoutTail(pid) ==
  (IF pid # Root /\ pid \notin nodes THEN <<>>
   ELSE Walk(pid, MaxItems + 1) \o Send("dbusmenu.go GetLayout: reply2(menu, call, ..)"))
  \o Unlock(Root, "dbusmenu.go GetLayout: deferred menu.m.Unlock()")

\* GetGroupProperties(nil, ..): RLock Menu.m, then each item in turn.
GroupHead == RLock(Root, "dbusmenu.go GetGroupProperties: menu.m.RLock()")
             \o <<Plan("Group", 0, 0, "dbusmenu.go GetGroupProperties: range over menu.nodes")>>
GroupTail ==
  LET ns == Sorted(nodes) IN
  Cat([i \in 1..Len(ns) |->
         RLock(ns[i], "dbusmenu.go GetGroupProperties: item.m.RLock()")
         \o RUnlock(ns[i], "dbusmenu.go GetGroupProperties: item.m.RUnlock()")])
  \o Send("dbusmenu.go GetGroupProperties: reply(menu, call, r)")
  \o RUnlock(Root, "dbusmenu.go GetGroupProperties: deferred menu.m.RUnlock()")

\* GetProperty(id, ..): RLock Menu.m, then the item.
PropHead(x) == RLock(Root, "dbusmenu.go GetProperty: menu.m.RLock()")
               \o <<Plan("Prop", x, 0, "dbusmenu.go GetProperty: menu.nodes[id]")>>
PropTail(x) ==
  (IF x \in nodes
   THEN RLock(x, "dbusmenu.go GetProperty: item.m.RLock()")
        \o Send("dbusmenu.go GetProperty: reply(menu, call, v)")
        \o RUnlock(x, "dbusmenu.go GetProperty: deferred item.m.RUnlock()")
   ELSE <<>>)
  \o RUnlock(Root, "dbusmenu.go GetProperty: deferred menu.m.RUnlock()")

\* Event(id, ..): event calls getHandler, which read-locks Menu.m and the
\* item and returns the handler. The handler then runs with whatever event
\* still holds. The modeled handler is an app's click handler that calls
\* SetProps on its own item.
EventHead(x) ==
  (IF EventHoldsMenuRead THEN RLock(Root, "dbusmenu.go event: menu.m.RLock()") ELSE <<>>)
  \o RLock(Root, "dbusmenu.go getHandler: menu.m.RLock()")
  \o <<Plan("Event", x, 0, "dbusmenu.go getHandler: menu.nodes[id]")>>
EventTail(x) ==
  (IF x \in nodes
   THEN RLock(x, "dbusmenu.go getHandler: item.m.RLock()")
        \o RUnlock(x, "dbusmenu.go getHandler: deferred item.m.RUnlock()")
   ELSE <<>>)
  \o RUnlock(Root, "dbusmenu.go getHandler: deferred menu.m.RUnlock()")
  \o (IF x \in nodes
      THEN <<[op |-> "handler", m |-> Root, k |-> "", a |-> x, b |-> 0,
              at |-> "dbusmenu.go event: h(eventID, data.Value(), timestamp)"]>>
      ELSE <<>>)
  \o (IF EventHoldsMenuRead THEN RUnlock(Root, "dbusmenu.go event: deferred menu.m.RUnlock()") ELSE <<>>)
  \o Reply

\* AboutToShow and AboutToShowGroup: RLock Menu.m around a lookup.
ShowProg == RLock(Root, "dbusmenu.go AboutToShow: menu.m.RLock()")
            \o RUnlock(Root, "dbusmenu.go AboutToShow: deferred menu.m.RUnlock()")
ShowGroupProg ==
  RLock(Root, "dbusmenu.go AboutToShowGroup: menu.m.RLock()")
  \o (IF ShowGroupCallsShow THEN ShowProg ELSE <<>>)
  \o RUnlock(Root, "dbusmenu.go AboutToShowGroup: deferred menu.m.RUnlock()")

\* The rest of a function once its plan step reads the tree.
Expand(st) ==
  CASE st.k = "SetProps"   -> SetPropsTail(st.a)
    [] st.k = "AddRoot"    -> AddRootTail(lastId + 1)
    [] st.k = "Add"        -> AddTail(st.a, lastId + 1)
    [] st.k = "Remove"     -> RemoveTail(st.a)
    [] st.k = "Append"     -> MoveTail(st.a, st.b, Parent(st.a), "appendChild")
    [] st.k = "MoveBefore" -> MoveTail(st.a, Parent(st.b), Parent(st.a), "MenuItem.MoveBefore")
    [] st.k = "GetLayout"  -> GetLayoutTail(st.a)
    [] st.k = "Group"      -> GroupTail
    [] st.k = "Prop"       -> PropTail(st.a)
    [] st.k = "Event"      -> EventTail(st.a)

-----------------------------------------------------------------------------
(* sync.RWMutex (Go src/sync/rwmutex.go). Lock takes the writer slot w,    *)
(* which stops new readers, then waits for the readers it found. RLock     *)
(* blocks while a writer holds w, so a goroutine that read-locks a mutex   *)
(* it already reads waits on that writer, which waits on it. Unlock lets   *)
(* the blocked readers in before it frees w. sync.Mutex is the same with   *)
(* Lock only.                                                              *)

Readers(m) == {q \in Procs : rd[m][q] > 0}

\* The tree change a plan step makes: newItem in both AddChild methods,
\* delete(item.menu.nodes, ..) in Remove, and setChildren with
\* child.parent = dst.getID() in appendChild and MoveBefore.
NewTree(st) ==
  CASE st.k \in {"AddRoot", "Add"} ->
         /\ nodes' = nodes \cup {lastId + 1}
         /\ par' = [par EXCEPT ![lastId + 1] = st.a]
         /\ lastId' = lastId + 1
    [] st.k = "Remove" /\ Parent(st.a) # Gone ->
         /\ nodes' = nodes \ {st.a}
         /\ UNCHANGED <<par, lastId>>
    [] st.k \in {"Append", "MoveBefore"} ->
         LET dst == IF st.k = "Append" THEN st.b ELSE Parent(st.b) IN
         \* The code takes the same locks for a move into the item's own
         \* subtree, but the cycle it makes sends buildLayout into endless
         \* recursion, a separate bug, so the model keeps the tree as it was.
         /\ par' = IF dst = Gone \/ Parent(st.a) = Gone \/ dst \in Below(st.a) THEN par
                   ELSE [par EXCEPT ![st.a] = dst]
         /\ UNCHANGED <<nodes, lastId>>
    [] OTHER -> UNCHANGED <<nodes, par, lastId>>

\* The rest of p's program after its next step, with a plan step that
\* follows expanded.
After(p) == LET r == Tail(prog[p]) IN
            IF r # <<>> /\ Head(r).op = "plan" THEN Expand(Head(r)) \o Tail(r) ELSE r

\* p finishes its next step, which may take Menu.m and so run a plan.
Advance(p) ==
  LET r == Tail(prog[p]) IN
  /\ prog' = [prog EXCEPT ![p] = After(p)]
  /\ IF r # <<>> /\ Head(r).op = "plan" THEN NewTree(Head(r))
     ELSE UNCHANGED <<nodes, par, lastId>>

Step(p) ==
  /\ prog[p] # <<>>
  /\ LET st == Head(prog[p]) IN
     CASE st.op = "Lock" /\ wr[st.m] = None ->
            \* Takes w and announces itself, so new readers block. With no
            \* readers to wait for it holds the lock at once.
            /\ wr' = [wr EXCEPT ![st.m] = p]
            /\ IF Readers(st.m) = {}
               THEN /\ wheld' = [wheld EXCEPT ![st.m] = TRUE]
                    /\ Advance(p)
               ELSE UNCHANGED <<prog, wheld, nodes, par, lastId>>
            /\ UNCHANGED <<rd, rwait>>
       [] st.op = "Lock" /\ wr[st.m] = p /\ ~wheld[st.m] /\ Readers(st.m) = {} ->
            /\ wheld' = [wheld EXCEPT ![st.m] = TRUE]
            /\ Advance(p)
            /\ UNCHANGED <<wr, rd, rwait>>
       [] st.op = "Unlock" ->
            /\ wr' = [wr EXCEPT ![st.m] = None]
            /\ wheld' = [wheld EXCEPT ![st.m] = FALSE]
            /\ rd' = [rd EXCEPT ![st.m] = [q \in Procs |-> IF q \in rwait[st.m] THEN @[q] + 1 ELSE @[q]]]
            /\ rwait' = [rwait EXCEPT ![st.m] = {}]
            \* Woken readers only plan reads, so the tree stays as it is.
            /\ prog' = [q \in Procs |-> IF q = p \/ q \in rwait[st.m] THEN After(q) ELSE prog[q]]
            /\ UNCHANGED <<nodes, par, lastId>>
       [] st.op = "RLock" /\ p \notin rwait[st.m] ->
            IF wr[st.m] = None
            THEN /\ rd' = [rd EXCEPT ![st.m][p] = @ + 1]
                 /\ Advance(p)
                 /\ UNCHANGED <<wr, wheld, rwait>>
            ELSE /\ rwait' = [rwait EXCEPT ![st.m] = @ \cup {p}]
                 /\ UNCHANGED <<prog, wr, wheld, rd, nodes, par, lastId>>
       [] st.op = "RUnlock" ->
            /\ rd' = [rd EXCEPT ![st.m][p] = @ - 1]
            /\ Advance(p)
            /\ UNCHANGED <<wr, wheld, rwait>>
       [] st.op = "handler" ->
            /\ prog' = [prog EXCEPT ![p] = SetPropsHead(st.a) \o Tail(@)]
            /\ UNCHANGED <<wr, wheld, rd, rwait, nodes, par, lastId>>
       [] OTHER -> FALSE
  /\ UNCHANGED <<ops, calls>>

\* What a blocked goroutine waits on.
WaitsOn(p) ==
  IF prog[p] = <<>> THEN {}
  ELSE LET st == Head(prog[p]) IN
       CASE st.op = "Lock" /\ wr[st.m] # None /\ (wr[st.m] # p \/ wheld[st.m]) -> {wr[st.m]}
         [] st.op = "Lock" /\ wr[st.m] = p /\ ~wheld[st.m] -> Readers(st.m)
         [] st.op = "RLock" /\ p \in rwait[st.m] -> {wr[st.m]}
         [] OTHER -> {}

Blocked == {p \in Procs : WaitsOn(p) # {}}

\* Goroutines that can never move again: each waits on one of the others.
Stuck == LET D[i \in 0..Cardinality(Procs)] ==
               IF i = 0 THEN Blocked
               ELSE {p \in D[i - 1] : WaitsOn(p) \cap D[i - 1] # {}}
         IN D[Cardinality(Procs)]

-----------------------------------------------------------------------------
(* Starting a call.                                                         *)

Idle(p) == prog[p] = <<>>
Known == 1..lastId

StartApp(p, pr) == /\ Idle(p) /\ ops < MaxOps
                   /\ prog' = [prog EXCEPT ![p] = pr]
                   /\ ops' = ops + 1
                   /\ UNCHANGED <<wr, wheld, rd, rwait, nodes, par, lastId, calls>>

StartCall(p, pr) == /\ Idle(p) /\ calls < MaxCalls
                    /\ prog' = [prog EXCEPT ![p] = pr]
                    /\ calls' = calls + 1
                    /\ UNCHANGED <<wr, wheld, rd, rwait, nodes, par, lastId, ops>>

\* An id the next AddChild can still take, counting AddChild calls
\* already started but not yet at newItem.
Adding == Cardinality({q \in Procs : \E i \in DOMAIN prog[q] :
                         prog[q][i].op = "plan" /\ prog[q][i].k \in {"AddRoot", "Add"}})
CanAdd == lastId + Adding < MaxItems

AppSetsProps == \E p \in AppProcs, x \in Known : StartApp(p, SetPropsHead(x))
AppAddsToMenu == CanAdd /\ \E p \in AppProcs : StartApp(p, AddRootHead)
AppAddsToItem == CanAdd /\ \E p \in AppProcs, x \in Known : StartApp(p, AddHead(x))
AppRemoves == \E p \in AppProcs, x \in Known : StartApp(p, RemoveHead(x))
AppAppends == \E p \in AppProcs, c \in Known, d \in {Root} \cup Known : StartApp(p, MoveHead(c, d, 0))
AppMovesBefore == \E p \in AppProcs, c, s \in Known : c # s /\ StartApp(p, MoveHead(c, 0, s))
AppReads == \E p \in AppProcs, x \in Known : StartApp(p, Getter(x))
AppSetsMenuHandler == \E p \in AppProcs : StartApp(p, MenuHandler)

HostGetsLayout == \E p \in CallProcs, x \in {Root} \cup Known : StartCall(p, GetLayoutHead(x))
HostGetsGroup == \E p \in CallProcs : StartCall(p, GroupHead)
HostGetsProperty == \E p \in CallProcs, x \in Known : StartCall(p, PropHead(x))
HostSendsEvent == \E p \in CallProcs, x \in Known : StartCall(p, EventHead(x))
HostShows == \E p \in CallProcs : StartCall(p, ShowProg \o Reply)
HostShowsGroup == \E p \in CallProcs : StartCall(p, ShowGroupProg \o Reply)

Run == \E p \in Procs : Step(p)

Init ==
  /\ prog = [p \in Procs |-> <<>>]
  /\ wr = [m \in Mutexes |-> None]
  /\ wheld = [m \in Mutexes |-> FALSE]
  /\ rd = [m \in Mutexes |-> [p \in Procs |-> 0]]
  /\ rwait = [m \in Mutexes |-> {}]
  /\ nodes = 1..InitItems
  \* Every tree of InitItems items. An item's parent has a lower id, which
  \* covers every shape up to renaming.
  /\ par \in {f \in [Ids -> {Root} \cup Ids] :
                \A i \in Ids : f[i] = Root \/ (i <= InitItems /\ f[i] < i)}
  /\ lastId = InitItems
  /\ ops = 0
  /\ calls = 0

Next ==
  \/ Run
  \/ AppSetsProps \/ AppAddsToMenu \/ AppAddsToItem \/ AppRemoves
  \/ AppAppends \/ AppMovesBefore \/ AppReads \/ AppSetsMenuHandler
  \/ HostGetsLayout \/ HostGetsGroup \/ HostGetsProperty \/ HostSendsEvent
  \/ HostShows \/ HostShowsGroup

Spec == Init /\ [][Next]_vars

\* Goroutines of one kind are interchangeable.
Symmetry == Permutations(AppProcs) \cup Permutations(CallProcs)

-----------------------------------------------------------------------------
(* Properties.                                                              *)

TypeOK ==
  /\ \A m \in Mutexes : wr[m] \in Procs \cup {None}
  /\ \A m \in Mutexes : \A q \in Procs : rd[m][q] \in Nat
  /\ nodes \subseteq Ids

\* A goroutine that finished its call holds no lock.
HoldsAny(p) == \E m \in Mutexes : rd[m][p] > 0 \/ wr[m] = p
NoLockLeft == \A p \in Procs : Idle(p) => ~HoldsAny(p)

\* No goroutine waits forever.
NoDeadlock == Stuck = {}

\* No goroutine asks for Menu.m while it holds or waits for an item lock.
HoldsItem(p) == \E i \in Ids : rd[i][p] > 0 \/ wr[i] = p
MenuLockedFirst ==
  \A p \in Procs :
    (prog[p] # <<>> /\ Head(prog[p]).op \in {"Lock", "RLock"} /\ Head(prog[p]).m = Root)
      => ~HoldsItem(p)

\* No goroutine read-locks a mutex it already holds.
NoRecursiveRead ==
  \A p \in Procs :
    (prog[p] # <<>> /\ Head(prog[p]).op = "RLock")
      => rd[Head(prog[p]).m][p] = 0 /\ wr[Head(prog[p]).m] # p

\* An app's MenuEventHandler runs with no lock held.
HandlerRunsUnlocked ==
  \A p \in Procs :
    (prog[p] # <<>> /\ Head(prog[p]).op = "handler") => ~HoldsAny(p)

=============================================================================
