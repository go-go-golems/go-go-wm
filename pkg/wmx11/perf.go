package wmx11

import (
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// suppressResizePaint drops decoration paint during a divider drag, leaving
// geometry commits in place. It is a measurement tool, not a feature: it
// answers "how much of the drag cost is paint?" directly, which is the
// question the chrome/content split (GGWM-012 Phase 4) exists to answer
// structurally. Panes keep their old pixels until the drag releases.
var suppressResizePaint = os.Getenv("GO_GO_WM_NO_RESIZE_PAINT") != ""

// snapOnRelease makes a divider track the pointer during the drag and apply
// the snap once, on release, instead of freezing inside each snap band.
// Default on: the frozen band was the behaviour users reported as the drag
// stopping (GGWM-012 Step 18). Set GO_GO_WM_SNAP_ON_RELEASE=0 for the old
// live-snapping feel.
// doubleBuffer renders each frame into a spare shared pixmap and swaps it in,
// so the server never composites from memory being written. Without it the
// background pixmap is written in place with no synchronisation, which tears
// (GGWM-012 Step 20). Costs a second surface per frame.
// GO_GO_WM_NO_DOUBLE_BUFFER=1 disables it.
var doubleBuffer = !envOn("GO_GO_WM_NO_DOUBLE_BUFFER")

func envOn(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// shmSync makes the WM wait for the server to finish processing the repair
// before the next paint may write the other buffer.
//
// MIT-SHM CompletionEvents do NOT apply here: the server emits them for
// ShmPutImage, and this design installs a shared pixmap as the window's
// background instead, which is what makes Expose repair free and server-side.
// The equivalent guarantee is a barrier — a reply-bearing request the server
// can only answer once it has processed everything queued behind it.
//
// Double buffering already prevents writing a buffer the server is reading in
// the same frame. This closes the remaining case where the server falls more
// than one frame behind. It costs one round trip per paint, so it is opt-in
// via GO_GO_WM_SHM_SYNC=1 and measured rather than assumed.
var shmSync = envOn("GO_GO_WM_SHM_SYNC")

var snapOnRelease = func() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GO_GO_WM_SNAP_ON_RELEASE"))) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}()

// perfCounters records the work the reconciler and paint path actually do.
//
// GGWM-012 found that the whole resize hot path was unmeasurable: the tree
// carried exactly two Debug-level timing probes and zero counters, so claims
// about where the time goes could not be settled. These counters exist to
// settle them. They are owned by the WM loop and read without locking from
// the same loop; the two fields that a foreign goroutine can touch are
// atomic.
//
// Cost when unused is a handful of integer increments per relayout.
type perfCounters struct {
	// Reconciliation
	relayouts           uint64
	layoutCalls         uint64 // wmcore.Layout invocations
	framesMoved         uint64 // geometry changed (position and/or size)
	framesResized       uint64 // size changed, so buffers are invalidated
	framesPainted       uint64
	mapReqSkipped       uint64 // Map/Unmap avoided by the mapped-state diff
	dividerPainted      uint64
	dividerPaintSkipped uint64

	// Upload path. shmCreates is the counter that decides GGWM-012's
	// central hypothesis: xshm.New performs two CHECKED X requests, i.e.
	// two synchronous round trips, and paintFrame recreates the surface on
	// every dimension change. During a steady drag this should be 0 once
	// capacity buffers land; today it is one per resized pane per tick.
	shmCreates   uint64
	shmDestroys  uint64
	ximgCreates  uint64 // the PutImage fallback's equivalent churn
	ximgDestroys uint64
	paintPixels  uint64

	// Drag scheduling
	motionEvents          uint64 // raw MotionNotify seen by a divider drag
	motionAdmitted        uint64 // ticks that actually did work
	resizePaintSuppressed uint64

	// Wall clock. The paint breakdown exists because measurement showed
	// ~98% of a paintFrame is neither fill nor text: it is colour
	// conversion plus upload, and until these are separated they cannot be
	// told apart (GGWM-012).
	paintNanos    uint64
	relayoutNanos uint64
	composeNanos  uint64 // fill + title + border into the RGBA scratch
	uploadNanos   uint64 // convert + surface create/attach + X blit
	convertNanos  uint64 // RGBA -> BGRA only
	syncNanos     uint64 // barrier round trips (GO_GO_WM_SHM_SYNC)
	syncWaits     uint64
	surfaceNanos  uint64 // surface destroy/create (the checked round trips)
}

// perfSnapshot is the JSON shape returned by the "perf" IPC query. It is a
// bounded aggregate on purpose: a debug endpoint must not stream an
// unbounded trace.
type perfSnapshot struct {
	Relayouts             uint64  `json:"relayouts"`
	LayoutCalls           uint64  `json:"layout_calls"`
	FramesMoved           uint64  `json:"frames_moved"`
	FramesResized         uint64  `json:"frames_resized"`
	FramesPainted         uint64  `json:"frames_painted"`
	MapReqSkipped         uint64  `json:"map_requests_skipped"`
	DividerPainted        uint64  `json:"divider_painted"`
	DividerPaintSkipped   uint64  `json:"divider_paint_skipped"`
	ShmCreates            uint64  `json:"shm_creates"`
	ShmDestroys           uint64  `json:"shm_destroys"`
	XimgCreates           uint64  `json:"ximg_creates"`
	XimgDestroys          uint64  `json:"ximg_destroys"`
	PaintMegapixels       float64 `json:"paint_megapixels"`
	MotionEvents          uint64  `json:"motion_events"`
	MotionAdmitted        uint64  `json:"motion_admitted"`
	MotionCoalesced       uint64  `json:"motion_coalesced"`
	ResizePaintSuppressed uint64  `json:"resize_paint_suppressed"`
	PaintMillis           float64 `json:"paint_ms_total"`
	ComposeMillis         float64 `json:"compose_ms_total"`
	UploadMillis          float64 `json:"upload_ms_total"`
	ConvertMillis         float64 `json:"convert_ms_total"`
	SurfaceMillis         float64 `json:"surface_ms_total"`
	SyncMillis            float64 `json:"sync_ms_total"`
	SyncWaits             uint64  `json:"sync_waits"`
	RelayoutMillis        float64 `json:"relayout_ms_total"`
	SharedPixmaps         bool    `json:"shared_pixmaps"`
	BufferBytes           int64   `json:"buffer_bytes"`
	BufferFrames          int     `json:"buffer_frames"`
}

func (p *perfCounters) snapshot(sharedPixmaps bool) perfSnapshot {
	coalesced := uint64(0)
	if p.motionEvents > p.motionAdmitted {
		coalesced = p.motionEvents - p.motionAdmitted
	}
	return perfSnapshot{
		Relayouts:             p.relayouts,
		LayoutCalls:           p.layoutCalls,
		FramesMoved:           p.framesMoved,
		FramesResized:         p.framesResized,
		FramesPainted:         p.framesPainted,
		MapReqSkipped:         p.mapReqSkipped,
		DividerPainted:        p.dividerPainted,
		DividerPaintSkipped:   p.dividerPaintSkipped,
		ShmCreates:            atomic.LoadUint64(&p.shmCreates),
		ShmDestroys:           atomic.LoadUint64(&p.shmDestroys),
		XimgCreates:           p.ximgCreates,
		XimgDestroys:          p.ximgDestroys,
		PaintMegapixels:       float64(p.paintPixels) / 1e6,
		MotionEvents:          p.motionEvents,
		MotionAdmitted:        p.motionAdmitted,
		MotionCoalesced:       coalesced,
		ResizePaintSuppressed: p.resizePaintSuppressed,
		PaintMillis:           float64(p.paintNanos) / 1e6,
		ComposeMillis:         float64(p.composeNanos) / 1e6,
		UploadMillis:          float64(p.uploadNanos) / 1e6,
		ConvertMillis:         float64(p.convertNanos) / 1e6,
		SurfaceMillis:         float64(p.surfaceNanos) / 1e6,
		SyncMillis:            float64(p.syncNanos) / 1e6,
		SyncWaits:             p.syncWaits,
		RelayoutMillis:        float64(p.relayoutNanos) / 1e6,
		SharedPixmaps:         sharedPixmaps,
	}
}

func (p *perfCounters) reset() { *p = perfCounters{} }

func (p *perfCounters) addPaint(d time.Duration, pixels int) {
	p.paintNanos += uint64(d.Nanoseconds())
	if pixels > 0 {
		p.paintPixels += uint64(pixels)
	}
}
