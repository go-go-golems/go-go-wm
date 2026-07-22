// shmprobe reports whether a display supports MIT-SHM shared pixmaps, which
// decides which upload path go-go-wm takes.
//
// go-go-wm logs this at Info on startup, but that requires taking over the
// display as window manager. This probe is an ordinary client, so it can be
// pointed at a live session.
//
//	go run ./scripts/shmprobe                  # $DISPLAY
//	DISPLAY=:2 go run ./scripts/shmprobe
package main

import (
	"fmt"
	"os"

	"github.com/jezek/xgb/shm"
	"github.com/jezek/xgbutil"
)

func main() {
	display := os.Getenv("DISPLAY")
	if len(os.Args) > 1 {
		display = os.Args[1]
	}
	X, err := xgbutil.NewConnDisplay(display)
	if err != nil {
		fmt.Printf("cannot connect to %q: %v\n", display, err)
		os.Exit(1)
	}
	defer X.Conn().Close()

	depth := X.Screen().RootDepth
	fmt.Printf("display        %s\n", display)
	fmt.Printf("root depth     %d\n", depth)

	if err := shm.Init(X.Conn()); err != nil {
		fmt.Printf("MIT-SHM        unavailable (%v)\n", err)
		verdict(false, depth)
		return
	}
	rep, err := shm.QueryVersion(X.Conn()).Reply()
	if err != nil {
		fmt.Printf("MIT-SHM        query failed (%v)\n", err)
		verdict(false, depth)
		return
	}
	fmt.Printf("MIT-SHM        %d.%d\n", rep.MajorVersion, rep.MinorVersion)
	fmt.Printf("SharedPixmaps  %v\n", rep.SharedPixmaps)
	verdict(rep.SharedPixmaps, depth)
}

func verdict(shared bool, depth byte) {
	fmt.Println()
	switch {
	case shared && depth == 24:
		fmt.Println("=> go-go-wm will use the MIT-SHM shared-pixmap path.")
	case shared:
		fmt.Printf("=> SharedPixmaps is available but root depth is %d, not 24,\n", depth)
		fmt.Println("   so go-go-wm falls back to PutImage (see xshm.Available).")
	default:
		fmt.Println("=> go-go-wm will use the PutImage fallback.")
		fmt.Println("   This is normal for an accelerated driver: with glamor, pixmaps")
		fmt.Println("   are GL textures in GPU memory and cannot be CPU-mapped shared")
		fmt.Println("   memory. It is not a misconfiguration.")
	}
}
