package wmx11

import (
	"os"
	"sync/atomic"
	"time"
)

// suppressResizePaint drops decoration paint during a divider drag, leaving
// geometry commits in place. It is a measurement tool, not a feature: it
// answers "how much of the drag cost is paint?" directly, which is the
// question the chrome/content split (GGWM-012 Phase 4) exists to answer
// structurally. Panes keep their old pixels until the drag releases.
var suppressResizePaint = os.Getenv("GO_GO_WM_NO_RESIZE_PAINT") != ""

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

	// Wall clock
	paintNanos    uint64
	relayoutNanos uint64
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
	RelayoutMillis        float64 `json:"relayout_ms_total"`
	SharedPixmaps         bool    `json:"shared_pixmaps"`
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
