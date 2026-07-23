#!/bin/bash
# ggwm-capsule-e2e.sh — end-to-end validation of semantic kernel v0 (GGWM-013).
#
# Drives the whole M1-M5 loop in a nested Xephyr: spawn an xterm, invoke
# the window.explain verb over the broker, assert the capsule + capability
# + broker lease exist, screenshot the capsule tile, close it, and assert
# zero residue. Exit code 0 = every assertion held.
set -uo pipefail
PARENT="${PARENT:-:0}"; NEST="${NEST:-:9}"
GO_GO_WM="${GO_GO_WM:?set GO_GO_WM to the binary}"
SOCK="${XDG_RUNTIME_DIR:-/tmp}/ggwm013.sock"; PBUI="${XDG_RUNTIME_DIR:-/tmp}/pbui013.sock"
SHOTS="${SHOTS:-$PWD}"
XP=""; WM=""
cleanup(){ [ -n "$WM" ] && kill $WM 2>/dev/null; sleep 0.4; [ -n "$XP" ] && kill $XP 2>/dev/null; rm -f "$SOCK" "$PBUI"; }
trap cleanup EXIT
fail(){ echo "!! FAIL: $*"; exit 1; }

rm -f "/tmp/.X${NEST#:}-lock" "$SOCK" "$PBUI"
DISPLAY=$PARENT Xephyr $NEST -screen 1280x800 -ac -noreset >/dev/null 2>&1 & XP=$!
for i in $(seq 60); do DISPLAY=$NEST xdotool getdisplaygeometry >/dev/null 2>&1 && break; sleep 0.25; done
"$GO_GO_WM" wm --display $NEST --embedded-broker --ipc-socket "$SOCK" --socket "$PBUI" >/dev/null 2>&1 & WM=$!
for i in $(seq 80); do [ -S "$SOCK" ] && break; sleep 0.1; done
[ -S "$SOCK" ] || fail "WM never came up"

ipc(){ python3 -c '
import socket,sys,json
s=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM); s.settimeout(5)
s.connect(sys.argv[1]); s.sendall((sys.argv[2]+"\n").encode())
print(s.makefile().readline().strip())' "$SOCK" "$1"; }

# pbui: NDJSON hello + one request on a raw socket.
pbui(){ python3 -c '
import socket,sys,json
s=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM); s.settimeout(5)
s.connect(sys.argv[1]); f=s.makefile("rw")
def send(m): f.write(json.dumps(m)+"\n"); f.flush()
def recv():
    return json.loads(f.readline())
send({"t":"hello","seq":1,"name":"e2e","roles":["app"],"protocol":1})
w=recv()
assert w["t"]=="welcome" and w.get("principal"), "no principal in welcome: %s"%w
send(json.loads(sys.argv[2]))
while True:
    r=recv()
    if r.get("seq")==2 or r["t"] in ("ok","error","resource.listing","verb.list"):
        print(json.dumps(r)); break' "$PBUI" "$1"; }

echo "== 1. xterm up, find its leaf"
DISPLAY=$NEST xterm -geometry 20x5 & sleep 1.5
leaf=$(ipc '{"q":"windows"}' | python3 -c '
import sys,json
for w in json.load(sys.stdin)["data"]:
    if w["client"]: print(w["leaf"]); break')
[ -n "$leaf" ] || fail "no client window"
echo "   leaf=$leaf"

echo "== 2. baseline sem state is empty"
ipc '{"q":"sem"}' | python3 -c '
import sys,json; d=json.load(sys.stdin)["data"]
assert d["capsules"]==[] and d["capabilities"]==[], d' || fail "baseline not clean"

echo "== 3. invoke window.explain via broker"
pbui "{\"t\":\"verb.invoke\",\"seq\":2,\"verb_id\":\"window.explain\",\"object\":{\"ptype\":\"tile\",\"value\":\"$leaf\"}}" \
  | grep -q '"t": *"ok"\|"t":"ok"' || fail "verb.invoke rejected"
sleep 2.5

echo "== 4. capsule live: sem state, broker lease, tile on screen"
ipc '{"q":"sem"}' | python3 -c '
import sys,json; d=json.load(sys.stdin)["data"]
assert len(d["capsules"])==1, "capsules: %s"%d["capsules"]
assert d["capsules"][0]["placed"], "capsule not placed"
assert len(d["capabilities"])==1, "caps: %s"%d["capabilities"]
c=d["capabilities"][0]
assert c["action"]=="wm.window.read" and c["holder"].startswith("capsule/")
print("   capsule:", d["capsules"][0]["id"], "ref:", d["capsules"][0]["ref"])' || fail "sem state after spawn"
pbui '{"t":"resource.list","seq":2}' | python3 -c '
import sys,json; r=json.loads(sys.stdin.read())
kinds=[x["kind"] for x in r.get("resources",[])]
assert "wm.capsule" in kinds, kinds
print("   broker lease present: wm.capsule")' || fail "broker resource missing"
sleep 0.5
DISPLAY=$NEST import -window root "$SHOTS/capsule-e2e-1-live.png" 2>/dev/null && echo "   shot: capsule-e2e-1-live.png"

echo "== 5. close the capsule tile; lease must end"
caps_leaf=$(ipc '{"q":"tree"}' | python3 -c '
import sys,json
d=json.loads(sys.stdin.read())["data"]
out=[]
def walk(n):
    if not isinstance(n,dict): return
    if n.get("kind")=="leaf":
        if str(n.get("app","")).startswith("script:explain"): out.append(n["id"])
        return
    walk(n.get("a")); walk(n.get("b"))
cur=d.get("current")
for w in d.get("workspaces",[]):
    if not cur or w["id"]==cur: walk(w["root"])
print(out[0] if out else "")')
[ -n "$caps_leaf" ] || fail "capsule leaf not in tree"
ipc "{\"q\":\"op\",\"op\":{\"op\":\"close-leaf\",\"node\":\"$caps_leaf\"}}" | grep -q '"ok":true' || fail "close-leaf failed"
sleep 1.5

echo "== 6. zero residue"
ipc '{"q":"sem"}' | python3 -c '
import sys,json; d=json.load(sys.stdin)["data"]
assert d["capsules"]==[], "capsules survived: %s"%d["capsules"]
assert d["capabilities"]==[], "capabilities survived: %s"%d["capabilities"]
print("   capsules=0 capabilities=0")' || fail "residue after close"
pbui '{"t":"resource.list","seq":2}' | python3 -c '
import sys,json; r=json.loads(sys.stdin.read())
kinds=[x["kind"] for x in r.get("resources",[])]
assert "wm.capsule" not in kinds, kinds
print("   broker lease gone")' || fail "broker lease survived"
DISPLAY=$NEST import -window root "$SHOTS/capsule-e2e-2-closed.png" 2>/dev/null && echo "   shot: capsule-e2e-2-closed.png"

echo "== 7. describe + tombstone via IPC"
ref=$(ipc '{"q":"windows"}' | python3 -c '
import sys,json
for w in json.load(sys.stdin)["data"]:
    if w["client"]: print("wm.window/0x%08x"%w["client"]); break')
ipc "{\"q\":\"describe\",\"ref\":\"$ref\"}" | python3 -c '
import sys,json; d=json.load(sys.stdin)["data"]
assert d["alive"], d
print("   describe:", d["ref"], d["title"])' || fail "describe live"
pkill -f "xterm -geometry 20x5" 2>/dev/null; sleep 1.2
ipc "{\"q\":\"describe\",\"ref\":\"$ref\"}" | python3 -c '
import sys,json; d=json.load(sys.stdin)["data"]
assert not d["alive"] and d["destroyed_at"], d
print("   tombstone:", d["ref"], "destroyed", d["destroyed_at"])' || fail "tombstone"

echo "== ALL PASS"
