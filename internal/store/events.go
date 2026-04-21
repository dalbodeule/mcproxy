package store

import (
	"context"
	"sync"
)

type auditEvent struct {
	serverID *int
	event    string
	details  map[string]any
}

type auditWriter struct {
	store *Store
	ch    chan auditEvent
	wg    sync.WaitGroup
}

func newAuditWriter(store *Store) *auditWriter {
	w := &auditWriter{store: store, ch: make(chan auditEvent, 1024)}
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		for evt := range w.ch {
			_ = w.store.writeAudit(context.Background(), evt.serverID, evt.event, evt.details)
		}
	}()
	return w
}

func (w *auditWriter) enqueue(evt auditEvent) {
	select {
	case w.ch <- evt:
	default:
		_ = w.store.writeAudit(context.Background(), evt.serverID, evt.event, evt.details)
	}
}

func (w *auditWriter) stop() {
	close(w.ch)
	w.wg.Wait()
}
