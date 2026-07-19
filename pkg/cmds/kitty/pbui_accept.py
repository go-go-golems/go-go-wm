#!/usr/bin/env python3
"""pbui_accept — a kitty kitten that turns the terminal into a PBUI accept
picker: it scans the screen for OSC 8 hyperlinks with pbui:// URIs matching
the pending accept's ptypes, overlays single-key hint labels (hints-kitten
style), and answers the broker with the chosen object via
`go-go-wm answer --uri ...`.

Invoked by the broker/WM through kitty remote control:

    kitten @ kitten pbui_accept.py --ptypes color,any --session s1

The interesting parts (scanning, filtering, ranking) are pure functions so
they unit-test against fake screen content with no kitty in the loop.
"""

import re
import subprocess
import sys

HINT_KEYS = "asdfghjkl;qwertyuiop"


def parse_args(argv):
    opts = {"ptypes": ["any"], "session": ""}
    it = iter(argv)
    for a in it:
        if a == "--ptypes":
            opts["ptypes"] = next(it, "any").split(",")
        elif a == "--session":
            opts["session"] = next(it, "")
    return opts


def ptype_of(uri):
    m = re.match(r"pbui://([^/]+)/", uri)
    return m.group(1) if m else None


def type_matches(want, have):
    return any(w == "any" or w == have for w in want)


def collect_candidates(hyperlinks, ptypes):
    """hyperlinks: iterable of (uri, line, col, text). Returns the matching
    subset, deduped by uri, ordered bottom-up (recent output first)."""
    seen = set()
    out = []
    for uri, line, col, text in hyperlinks:
        pt = ptype_of(uri)
        if pt is None or not type_matches(ptypes, pt):
            continue
        if uri in seen:
            continue
        seen.add(uri)
        out.append({"uri": uri, "line": line, "col": col, "text": text, "ptype": pt})
    out.sort(key=lambda c: -c["line"])
    return out[: len(HINT_KEYS)]


def assign_hints(candidates):
    return {HINT_KEYS[i]: c for i, c in enumerate(candidates)}


# --- kitty-facing glue (kept thin) -----------------------------------------

def screen_hyperlinks(screen):
    """Extract (uri, line, col, text) tuples from a kitty Screen object."""
    links = []
    try:
        for y in range(screen.lines):
            line = screen.line(y)
            # kitty exposes hyperlink ids per cell; walk runs of equal ids.
            run_uri, run_start, run_text = None, 0, ""
            for x in range(screen.columns):
                uri = None
                hid = line.hyperlink_ids[x] if hasattr(line, "hyperlink_ids") else 0
                if hid:
                    uri = screen.hyperlink_for_id(hid)
                ch = str(line[x])
                if uri != run_uri:
                    if run_uri:
                        links.append((run_uri, y, run_start, run_text))
                    run_uri, run_start, run_text = uri, x, ""
                if uri:
                    run_text += ch
            if run_uri:
                links.append((run_uri, y, run_start, run_text))
    except Exception:
        pass
    return links


def main(args):
    # Executed as a kitten: draw a picker over the screen.
    opts = parse_args(args[1:])
    print("\x1b[2J\x1b[H", end="")
    print("PBUI ACCEPT <%s> — press a key to answer, Esc aborts\r\n"
          % "|".join(opts["ptypes"]))
    cands = getattr(main, "_candidates", [])
    hints = assign_hints(cands)
    for key, c in hints.items():
        print("  [%s] <%s> %s\r\n" % (key, c["ptype"], c["text"] or c["uri"]))
    sys.stdout.flush()
    try:
        import tty, termios
        fd = sys.stdin.fileno()
        old = termios.tcgetattr(fd)
        tty.setraw(fd)
        ch = sys.stdin.read(1)
        termios.tcsetattr(fd, termios.TCSADRAIN, old)
    except Exception:
        ch = ""
    c = hints.get(ch)
    if c is None:
        return None
    return c["uri"]


def handle_result(args, answer, target_window_id, boss):
    # Runs in the kitty process after main() returns.
    if not answer:
        return
    opts = parse_args(args[1:])
    cmd = ["go-go-wm", "answer", "--uri", answer]
    if opts["session"]:
        cmd += ["--session", opts["session"]]
    subprocess.Popen(cmd)


def on_load(boss):
    pass


if __name__ == "__main__":
    # Self-test mode: feed fake hyperlinks on stdin as "uri\tline\tcol\ttext".
    fake = []
    for raw in sys.stdin:
        parts = raw.rstrip("\n").split("\t")
        if len(parts) == 4:
            fake.append((parts[0], int(parts[1]), int(parts[2]), parts[3]))
    cands = collect_candidates(fake, sys.argv[1].split(",") if len(sys.argv) > 1 else ["any"])
    for key, c in assign_hints(cands).items():
        print("%s\t%s\t%s" % (key, c["ptype"], c["uri"]))
