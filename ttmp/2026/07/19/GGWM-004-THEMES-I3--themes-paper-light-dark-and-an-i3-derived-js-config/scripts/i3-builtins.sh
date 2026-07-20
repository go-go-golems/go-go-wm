#!/bin/bash
set -u
export GO_GO_WM_SOCKET=/tmp/claude-1000/ggwm004/wm.sock
D=:79
S=/tmp/claude-1000/ggwm004/shots
ipc() { python3 -c '
import json,socket,os,sys
s=socket.socket(socket.AF_UNIX); s.connect(os.environ["GO_GO_WM_SOCKET"])
s.sendall((sys.argv[1]+"\n").encode()); print(s.makefile().readline().strip()[:120])' "$1"; }
ipc '{"q":"op","op":{"op":"set-leaf-app","node":"n2","app":"builtin:trace"}}'
ipc '{"q":"op","op":{"op":"split-leaf","node":"n2","dir":"row","app":"builtin:listener"}}'
ipc '{"q":"op","op":{"op":"switch-workspace","workspace":"ws2"}}'
sleep 1
DISPLAY=$D import -window root $S/v2-dark-builtins.png
ipc '{"q":"set-theme","theme":"light"}'; sleep 1.2
DISPLAY=$D import -window root $S/v2-light-builtins.png
ipc '{"q":"set-theme","theme":"paper"}'; sleep 1.2
DISPLAY=$D import -window root $S/v2-paper-builtins.png
ipc '{"q":"set-theme","theme":"dark"}'; sleep 1.2
DISPLAY=$D import -window root $S/v2-dark2-builtins.png
for f in v2-dark-builtins v2-light-builtins v2-paper-builtins v2-dark2-builtins; do python3 -c "
from PIL import Image
im = Image.open('$S/$f.png').convert('RGB')
print('$f', 'bar:', im.getpixel((500,10)), 'pane:', im.getpixel((300,400)))"; done
