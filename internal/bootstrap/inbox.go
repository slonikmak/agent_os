package bootstrap

import (
	"context"
	"personaldesk/internal/application/queries"
)

func (w *Workspace) GetInboxMessage(ctx context.Context, id string) (queries.InboxDetails, error) {
	if w.lifetime.Err() != nil {
		return queries.InboxDetails{}, queries.Unavailable()
	}
	merged, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(w.lifetime, cancel)
	defer cancel()
	defer stop()
	return w.reader.(queries.InboxReader).GetInboxMessage(merged, id)
}

// SubscribeChanges has an explicit native lifecycle owner. The lock makes stop
// wait for an in-flight callback, so teardown cannot race a late publication.
// Callbacks must be short and must not subscribe/unsubscribe recursively.
func (w *Workspace) SubscribeChanges(callback func(int64)) func() {
	w.changesMu.Lock()
	defer w.changesMu.Unlock()
	if w.lifetime.Err() != nil {
		return func() {}
	}
	w.nextSubscriber++
	id := w.nextSubscriber
	w.subscribers[id] = callback
	return func() { w.changesMu.Lock(); delete(w.subscribers, id); w.changesMu.Unlock() }
}
func (w *Workspace) publishRevision(revision int64) {
	w.changesMu.Lock()
	defer w.changesMu.Unlock()
	if w.lifetime.Err() != nil {
		return
	}
	for _, callback := range w.subscribers {
		func() {
			defer func() {
				if recover() != nil {
					w.diag.Fault("bootstrap", "framework", "", "panic")
				}
			}()
			callback(revision)
		}()
	}
}
