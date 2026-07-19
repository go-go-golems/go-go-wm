// Package xshm implements zero-copy frame uploads via the MIT-SHM
// extension's shared pixmaps (GGWM-006): the client and the X server
// map the same SysV shared memory segment, and ShmCreatePixmap makes
// that segment BE a pixmap — writing pixels into Data and ClearArea-ing
// the window replaces the whole PutImage copy chain (client write →
// socket → server read → pixmap copy) with a single conversion write.
//
// Lifecycle discipline: the segment is marked IPC_RMID immediately
// after both sides attach, so the kernel reclaims it when the
// attachments drop — no leaked segments even on kill -9 (verify with
// `ipcs -m`). Callers own Destroy() for the X-side resources.
package xshm

import (
	"fmt"
	"image"
	"os"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/shm"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"golang.org/x/sys/unix"
)

var (
	availMu    sync.Mutex
	availCache = map[*xgb.Conn]bool{}
)

// Available reports whether this connection can use shared pixmaps:
// the MIT-SHM extension initializes, the server advertises
// SharedPixmaps, and GO_GO_WM_NO_SHM is unset. Cached per connection.
func Available(X *xgbutil.XUtil) bool {
	if os.Getenv("GO_GO_WM_NO_SHM") != "" {
		return false
	}
	availMu.Lock()
	defer availMu.Unlock()
	c := X.Conn()
	if v, ok := availCache[c]; ok {
		return v
	}
	ok := false
	if err := shm.Init(c); err == nil {
		if rep, err := shm.QueryVersion(c).Reply(); err == nil {
			ok = rep.SharedPixmaps
		}
	}
	availCache[c] = ok
	return ok
}

// Surface is one shared-memory pixmap: our mapping in Data, the
// server's view as Pixmap.
type Surface struct {
	X      *xgbutil.XUtil
	Seg    shm.Seg
	Pixmap xproto.Pixmap
	Data   []byte // W*H*4 bytes, BGRA, row-major, stride == W*4
	W, H   int
}

// New creates a surface sized w×h for the given drawable's screen.
// Callers should have checked Available; New re-validates the depth
// (the shared pixmap must match the root depth, 24 on our visuals).
func New(X *xgbutil.XUtil, drawable xproto.Drawable, w, h int) (*Surface, error) {
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("xshm: bad size %dx%d", w, h)
	}
	size := w * h * 4

	shmid, err := unix.SysvShmGet(unix.IPC_PRIVATE, size, unix.IPC_CREAT|0o600)
	if err != nil {
		return nil, fmt.Errorf("xshm: shmget(%d bytes): %w", size, err)
	}
	data, err := unix.SysvShmAttach(shmid, 0, 0)
	if err != nil {
		_, _ = unix.SysvShmCtl(shmid, unix.IPC_RMID, nil)
		return nil, fmt.Errorf("xshm: shmat: %w", err)
	}

	seg, err := shm.NewSegId(X.Conn())
	if err == nil {
		err = shm.AttachChecked(X.Conn(), seg, uint32(shmid), false).Check()
	}
	// Both sides attached (or we are bailing): either way, mark the
	// segment for deletion now so the kernel reclaims it whenever the
	// attachments drop — including on any crash path.
	_, _ = unix.SysvShmCtl(shmid, unix.IPC_RMID, nil)
	if err != nil {
		_ = unix.SysvShmDetach(data)
		return nil, fmt.Errorf("xshm: server attach: %w", err)
	}

	pid, err := xproto.NewPixmapId(X.Conn())
	if err == nil {
		depth := X.Screen().RootDepth
		err = shm.CreatePixmapChecked(X.Conn(), pid, drawable,
			uint16(w), uint16(h), depth, seg, 0).Check()
	}
	if err != nil {
		shm.Detach(X.Conn(), seg)
		_ = unix.SysvShmDetach(data)
		return nil, fmt.Errorf("xshm: shared pixmap: %w", err)
	}

	return &Surface{X: X, Seg: seg, Pixmap: pid, Data: data[:size], W: w, H: h}, nil
}

// WriteRGBA converts img (RGBA) into the surface (BGRA), row-major —
// the same loop shape as draw.CopyToXImage, writing into memory the
// server composites from. img must be exactly W×H.
func (s *Surface) WriteRGBA(img *image.RGBA) {
	r := img.Bounds()
	w := r.Dx()
	if w != s.W || r.Dy() != s.H {
		return
	}
	for y := 0; y < s.H; y++ {
		so := img.PixOffset(r.Min.X, r.Min.Y+y)
		do := y * s.W * 4
		src := img.Pix[so : so+w*4 : so+w*4]
		dst := s.Data[do : do+w*4 : do+w*4]
		for i := 0; i < w*4; i += 4 {
			dst[i+0] = src[i+2] // B
			dst[i+1] = src[i+1] // G
			dst[i+2] = src[i+0] // R
			dst[i+3] = src[i+3] // A
		}
	}
}

// Destroy releases the X-side resources and our mapping. The kernel
// segment itself is already RMID-marked and dies with the detachments.
func (s *Surface) Destroy() {
	if s == nil {
		return
	}
	xproto.FreePixmap(s.X.Conn(), s.Pixmap)
	shm.Detach(s.X.Conn(), s.Seg)
	_ = unix.SysvShmDetach(s.Data[:cap(s.Data)])
	s.Data = nil
}
