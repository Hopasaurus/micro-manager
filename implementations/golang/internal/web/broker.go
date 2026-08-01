package web

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"micromanager/internal/web/api"
	"micromanager/mm"
)

// The SSE broker (project/architecture.md §4.5).
//
// One goroutine per OPEN PROJECT polls the library fingerprint
// (spec-tools.md §2.4) and publishes named events when it changes. It is a
// fan-out over an existing signal, not a second source of truth: the same
// fingerprint a client would poll, one poller, many subscribers.
//
// theme.json and config.json are deliberately outside the fingerprint
// (mm/fingerprint.go), so the poller checks the theme file's stamp directly —
// a theme edit must reach every open tab (§8.7), and it would never arrive
// through the data hash.
//
// Subscribers hold bounded buffered channels. A subscriber that cannot keep up
// is DROPPED and its client reconnects: a slow reader must never stall a
// mutation, and an SSE client is built to reconnect anyway.

// broker fans named events out to per-project subscribers.
type broker struct {
	mu       sync.Mutex
	subs     map[string]map[*subscriber]struct{} // projectID -> subscribers
	cancels  map[string]context.CancelFunc       // projectID -> poller cancel
	registry *registry
	interval time.Duration

	rootCtx    context.Context
	rootCancel context.CancelFunc
	wg         sync.WaitGroup

	// closed is set by close, under mu, before it waits. Subscribe refuses
	// once it is set, which is what keeps wg.Add out of wg.Wait's way.
	closed bool
}

// subscriber is one open event stream.
type subscriber struct {
	ch chan api.Event
}

func newBroker(reg *registry, interval time.Duration) *broker {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &broker{
		subs:       map[string]map[*subscriber]struct{}{},
		cancels:    map[string]context.CancelFunc{},
		registry:   reg,
		interval:   interval,
		rootCtx:    ctx,
		rootCancel: cancel,
	}
}

// Subscribe opens a stream for one project, starting the project's poller on
// the first subscriber. The returned function unsubscribes; a stream must
// always unsubscribe, or its poller runs forever.
//
// After close, it hands back an already-closed channel instead of registering.
// That is not politeness to a late caller: wg.Add MUST NOT run once close has
// entered wg.Wait with the counter at zero, or the runtime panics with
// "WaitGroup misuse: Add called concurrently with Wait" and takes the process
// down as it shuts down. The closed flag is read under the same lock Subscribe
// already holds, so there is no window between the check and the Add.
func (b *broker) Subscribe(projectID string) (<-chan api.Event, func()) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		dead := make(chan api.Event)
		close(dead)
		return dead, func() {}
	}
	if b.subs[projectID] == nil {
		b.subs[projectID] = map[*subscriber]struct{}{}
	}
	if len(b.subs[projectID]) == 0 {
		ctx, cancel := context.WithCancel(b.rootCtx)
		b.cancels[projectID] = cancel
		b.wg.Add(1)
		go b.poll(ctx, projectID)
	}
	sub := &subscriber{ch: make(chan api.Event, 8)}
	b.subs[projectID][sub] = struct{}{}
	b.mu.Unlock()

	return sub.ch, func() { b.unsubscribe(projectID, sub) }
}

func (b *broker) unsubscribe(projectID string, sub *subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subs[projectID] == nil {
		return
	}
	delete(b.subs[projectID], sub)
	if len(b.subs[projectID]) == 0 {
		// The last stream closed: the poller has nobody to tell.
		delete(b.subs, projectID)
		if cancel, ok := b.cancels[projectID]; ok {
			cancel()
			delete(b.cancels, projectID)
		}
	}
}

// publish sends events to every subscriber of a project.
//
// A subscriber whose buffer is full is dropped, and its channel is closed so
// the SSE handler exits and the client reconnects with a fresh buffer.
func (b *broker) publish(projectID string, events ...api.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for sub := range b.subs[projectID] {
		for _, ev := range events {
			select {
			case sub.ch <- ev:
			default:
				delete(b.subs[projectID], sub)
				close(sub.ch)
				goto next
			}
		}
	next:
	}
}

// close stops every poller and waits for them to exit. Called when the service
// stops, and safe to call more than once.
//
// The flag goes up UNDER THE LOCK and before the wait, so every Subscribe
// either completed its Add already (and its poller is one of the ones being
// waited for) or will take the closed branch and never Add at all.
func (b *broker) close() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()

	b.rootCancel()
	b.wg.Wait()
}

// poll watches one project's data and publishes named events on change.
func (b *broker) poll(ctx context.Context, projectID string) {
	defer b.wg.Done()

	store, err := b.registry.resolve(projectID)
	if err != nil {
		return
	}

	// Baseline BEFORE the loop: a client that just connected already has fresh
	// state, and publishing on the first tick would make it fetch the very
	// thing it was served a moment ago.
	var last mm.Fingerprint
	if fp, err := store.Fingerprint(); err == nil {
		last = fp
	}
	lastTheme, _ := themeStamp(store)

	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fp, err := store.Fingerprint()
			if err == nil && fp != last {
				last = fp
				// The data files are the input to all three regions: column
				// counts, WIP and the validation result are computed from the
				// same lines, so a change to any of them invalidates all three
				// at once.
				b.publish(projectID,
					api.Event{Name: "board"},
					api.Event{Name: "status"},
					api.Event{Name: "check"},
				)
			}
			// Any KNOWN change to the theme file is an event: written,
			// edited, or deleted. A deletion drops the project back to the
			// system theme, which is as much a re-theme as an edit is.
			if mt, known := themeStamp(store); known && mt != lastTheme {
				lastTheme = mt
				b.publish(projectID, api.Event{Name: "theme"})
			}
		case <-ctx.Done():
			return
		}
	}
}

// themeStamp is the project theme file's modification time, and whether that
// answer is KNOWN. A project with no theme resolves system/builtin; only the
// project file's own change is this project's concern.
//
// The second return is what separates "there is definitely no theme file" —
// a zero time the poller must act on, because deleting theme.json re-themes
// every open tab exactly as writing one does — from "could not tell", where a
// zero time means nothing. Collapsing the two into a bare zero time is what
// made a deletion invisible: the poller could not distinguish it from a
// transient stat failure, so it ignored both.
func themeStamp(store *mm.Store) (time.Time, bool) {
	d, err := store.Directory()
	if err != nil {
		return time.Time{}, false
	}
	fi, err := os.Stat(filepath.Join(d.Path, "theme.json"))
	if err != nil {
		// Absent is an answer. Any other stat error is not: treating it as a
		// deletion would flap a theme event on every tick until it cleared.
		return time.Time{}, os.IsNotExist(err)
	}
	return fi.ModTime(), true
}
