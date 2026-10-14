package store

import (
	"context"
	"fmt"
	"sync"

	"go.minekube.com/common/minecraft/component"
)

type auditEvent struct {
	serverID *int
	actor    *string
	event    string
	details  map[string]any
}

type auditWriter struct {
	store   *Store
	ch      chan auditEvent
	wg      sync.WaitGroup
	mu      sync.RWMutex
	stopped bool
}

func newAuditWriter(store *Store) *auditWriter {
	w := &auditWriter{store: store, ch: make(chan auditEvent, 1024)}
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		for evt := range w.ch {
			_ = w.store.writeAudit(context.Background(), evt.actor, evt.serverID, evt.event, evt.details)
		}
	}()
	return w
}

func (w *auditWriter) enqueue(evt auditEvent) {
	w.mu.RLock()
	if w.stopped {
		w.mu.RUnlock()
		w.store.auditWriteErrors.Add(1)
		return
	}
	select {
	case w.ch <- evt:
		w.mu.RUnlock()
		return
	default:
	}
	w.mu.RUnlock()
	_ = w.store.writeAudit(context.Background(), evt.actor, evt.serverID, evt.event, evt.details)
}

func (w *auditWriter) stop() {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		w.wg.Wait()
		return
	}
	w.stopped = true
	close(w.ch)
	w.mu.Unlock()
	w.wg.Wait()
}

// sanitizeAuditValue removes protocol/component values before Ent hands the
// details map to encoding/json. Some Minecraft component types intentionally
// panic from MarshalJSON and require Gate's codec instead. Audit persistence
// must never be able to take down the proxy when a backend sends such a
// disconnect reason.
func sanitizeAuditValue(value any) any {
	switch v := value.(type) {
	case nil, string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return v
	case *component.Text:
		if v == nil {
			return nil
		}
		return v.Content
	case *component.Translation:
		if v == nil {
			return nil
		}
		return v.Key
	case component.Component:
		return fmt.Sprintf("%T", v)
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = sanitizeAuditValue(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = sanitizeAuditValue(item)
		}
		return out
	default:
		// Keep audit payloads conservative. The known audit callers only use
		// JSON scalar values, maps, and slices; unknown values are represented
		// by their type instead of invoking a potentially unsafe marshaler.
		return fmt.Sprintf("%T", value)
	}
}
