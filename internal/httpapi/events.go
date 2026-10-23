package httpapi

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/pubsub"
)

// serveEvents streams change notifications to browsers (Server-Sent Events). The
// frontend refreshes server-rendered data when it sees a newer config version.
// Streams end when the client leaves or the broker closes (server shutdown).
func serveEvents(d Deps) http.HandlerFunc {
	limit := int64(d.MaxEventStreams)
	if limit <= 0 {
		limit = defaultMaxEventStreams
	}
	var open atomic.Int64
	return func(w http.ResponseWriter, r *http.Request) {
		if open.Add(1) > limit {
			open.Add(-1)
			w.Header().Set("Retry-After", "30")
			http.Error(w, "too many event streams", http.StatusServiceUnavailable)
			return
		}
		defer open.Add(-1)

		rc := http.NewResponseController(w)
		_ = rc.SetWriteDeadline(time.Time{}) // long-lived stream
		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		h.Set("X-Accel-Buffering", "no") // nginx: do not buffer

		ch, cancel := d.Broker.Subscribe(pubsub.TopicConfigChanged, 4)
		defer cancel()

		fmt.Fprintf(w, "retry: 3000\nevent: hello\ndata: {\"version\":%d}\n\n", d.Settings.Current().Version)
		if err := rc.Flush(); err != nil {
			return
		}
		ping := time.NewTicker(25 * time.Second)
		defer ping.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return // broker closed: server is shutting down
				}
				fmt.Fprintf(w, "event: config.changed\ndata: {\"version\":%d}\n\n", msg.Data)
			case <-ping.C:
				fmt.Fprint(w, ": ping\n\n")
			}
			if err := rc.Flush(); err != nil {
				return
			}
		}
	}
}
