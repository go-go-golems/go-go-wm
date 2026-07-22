#!/usr/bin/env python3
"""Compare paintFrame / afterOp timings between the shm-on and shm-off runs.

Usage: analyze-shm-ab.py ~/ggwm-shm-ab
Reads shm-on.jsonl and shm-off.jsonl, restricts to the drag window delimited
by the __marker lines, and reports duration distributions.
"""
import json, sys, os, statistics as st

def pct(xs, p):
    if not xs: return float('nan')
    xs = sorted(xs); k = (len(xs)-1)*p/100.0
    lo, hi = int(k//1), min(int(k//1)+1, len(xs)-1)
    return xs[lo] + (xs[hi]-xs[lo])*(k-lo)

def load(path):
    """Return (paint_ms, afterop_ms, sizes) inside the drag window."""
    paint, afterop, sizes = [], [], []
    inwin = False
    with open(path, encoding='utf-8', errors='replace') as fh:
        for line in fh:
            line = line.strip()
            if not line: continue
            try: r = json.loads(line)
            except Exception: continue
            m = r.get('__marker')
            if m == 'drag_start': inwin = True; continue
            if m == 'drag_end':   inwin = False; continue
            if not inwin: continue
            msg = r.get('message') or r.get('msg') or ''
            # zerolog Dur("ms", ...) -> numeric field named "ms"
            v = r.get('ms')
            if v is None: continue
            try: v = float(v)
            except Exception: continue
            if msg == 'paintFrame':
                paint.append(v)
                w, h = r.get('w'), r.get('h')
                if isinstance(w, (int, float)) and isinstance(h, (int, float)):
                    sizes.append((int(w), int(h)))
            elif msg == 'afterOp':
                afterop.append(v)
    return paint, afterop, sizes

def summarize(label, paint, afterop, sizes):
    print(f"\n--- {label} ---")
    if not paint:
        print("  no paintFrame samples (is --log-level debug set? did the drag hit a divider?)")
    else:
        print(f"  paintFrame  n={len(paint):5d}  "
              f"p50={pct(paint,50):7.3f}ms  p95={pct(paint,95):7.3f}ms  "
              f"p99={pct(paint,99):7.3f}ms  max={max(paint):7.3f}ms  "
              f"total={sum(paint):8.1f}ms")
        if sizes:
            uniq = len(set(sizes))
            px = sum(w*h for w, h in sizes)
            print(f"              distinct pane sizes={uniq}  "
                  f"(a high count means every tick changed dimensions -> cache miss每tick)"
                  .replace("每tick", " per tick"))
            print(f"              pixels painted={px/1e6:.1f} Mpx  "
                  f"~{px*4/1e6:.1f} MB RGBA written")
    if afterop:
        print(f"  afterOp     n={len(afterop):5d}  "
              f"p50={pct(afterop,50):7.3f}ms  p95={pct(afterop,95):7.3f}ms  "
              f"max={max(afterop):7.3f}ms")
    return paint

def main():
    base = sys.argv[1] if len(sys.argv) > 1 else os.path.expanduser('~/ggwm-shm-ab')
    runs = {}
    for label in ('shm-on', 'shm-off'):
        p = os.path.join(base, f'{label}.jsonl')
        if not os.path.exists(p):
            print(f"missing {p}"); continue
        runs[label] = load(p)
        summarize(label, *runs[label])

    if len(runs) == 2:
        a = runs['shm-on'][0]; b = runs['shm-off'][0]
        print("\n=== VERDICT ===")
        if not a or not b:
            print("  insufficient samples in one or both runs.")
            return
        ap, bp = pct(a, 95), pct(b, 95)
        print(f"  paintFrame p95:  shm-on={ap:.3f}ms   shm-off={bp:.3f}ms   "
              f"ratio={ap/bp if bp else float('nan'):.2f}x")
        if ap > bp * 1.15:
            print("  → shm-on is SLOWER. Supports the hypothesis: the per-tick")
            print("    surface destroy/recreate (2 checked round trips) costs more")
            print("    than the zero-copy upload saves during resize.")
            print("    Act on Phase 2 (capacity buffers / suppress paint during drag).")
        elif bp > ap * 1.15:
            print("  → shm-on is FASTER. REFUTES the hypothesis as the dominant cost.")
            print("    Re-order the plan: put Phase 4 (chrome/content split) first,")
            print("    and update Part IV of the design doc accordingly.")
        else:
            print("  → No clear difference. The round trips are real but not dominant")
            print("    at this resolution (1280x800). Re-run at 1920x1080 before")
            print("    concluding; pixel cost scales ~2.3x, round-trip cost does not.")
        print("\n  NOTE: paintFrame's timer wraps the surface recreate, so this is an")
        print("  indirect measure. For a direct count, add counters to xshm.New/Destroy")
        print("  (Phase 0) and compare shm_creates per drag.")

if __name__ == '__main__':
    main()
