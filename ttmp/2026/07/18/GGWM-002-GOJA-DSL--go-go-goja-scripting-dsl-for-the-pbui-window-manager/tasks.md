# Tasks

## TODO

- [x] P1: pbui native module + go-go-wm run [--once] (engine.New, promise accept, verbs) <!-- t:12c4 -->
- [x] P2: wm native module over control socket (queries, apply, sugar, events) <!-- t:jcnl -->
- [x] P3: in-process JS owner loop + rc.js + wm.bind + go-go-wm repl (replapi) <!-- t:42zp -->
- [x] P4: rules + layout recipes (normalize->compile), script.error events, examples/scripts/ <!-- t:79mh -->
- [ ] P5 (later): ui module emitting Region IR; xgoja provider packaging <!-- t:0skp -->
- [x] P1: pkg/jsmod/bridge.go + errors.go (wire<->JS conversion, script.error) + fuzz test <!-- t:6h5v -->
- [x] P1: pkg/jsmod/pbuimod (accept promise pattern, verbs, print, on with bounded queue, data-only helpers) <!-- t:npmm -->
- [x] P1: pkg/cmds/run.go with --once and --allow-exec/--allow-spawn capability flags <!-- t:1cin -->
- [x] P1: pbuimod test suite against bare broker (accept/cancel/verb/print/overflow) <!-- t:2r23 -->
- [x] P2: wmmod Backend interface + ipcBackend + sugar.go (Ops compilation) + events.go <!-- t:ykd7 -->
- [x] P2: wmmod unit tests (fake backend Op-sequence assertions, mirror-desktop property test) <!-- t:1ymf -->
- [x] P2: A2 Xvfb integration: golden.js self-asserting via wm.tree() <!-- t:fsyl -->
- [x] P3: inprocBackend + --rc flag + wm.bind + repl.go over replapi + rc-smoke Xvfb test <!-- t:cdnb -->
- [x] P4: rules.go + layouts (normalize->compile, wm.rules()/wm.layouts() inspection) + project-switcher.js + help topics <!-- t:4e4m -->
- [x] Examples in examples/scripts/ wired into CI as fixtures <!-- t:md2t -->
