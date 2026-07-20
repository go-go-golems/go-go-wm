// git-verbs.js — teach the whole desktop new verbs. [works today: P1]
//
//   go-go-wm run examples/scripts/git-verbs.js          # daemon
//
// After this loads, every git-commit presentation anywhere on the desktop
// — a hash scraped out of a kitty terminal, a commit printed by another
// script — gains these entries in its right-click menu. The handler runs
// here, in this process; the broker routes the invocation.

const pbui = require("pbui");

pbui.verb(
  { id: "git.copy-short", label: "Copy short hash", ptypes: ["git-commit"] },
  (commit) => {
    pbui.print("short hash: ", pbui.object("git-commit", commit.value.slice(0, 7)));
  }
);

pbui.verb(
  { id: "git.compare-with", label: "Compare with… (accept a commit)",
    ptypes: ["git-commit"], accepts: ["git-commit"] },
  async (commit) => {
    // Verbs can accept: click this on one commit, then click another.
    const other = await pbui.accept("git-commit", "COMPARE — click the other commit");
    if (other === null) return;
    pbui.print(
      "git range  ",
      pbui.object("git-commit", commit.value.slice(0, 7)),
      "..",
      pbui.object("git-commit", other.value.slice(0, 7))
    );
  }
);

pbui.print("git-verbs loaded: git-commit objects now have 2 new verbs");
