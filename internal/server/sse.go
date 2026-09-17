package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/arthurr0/backvault/internal/engine"
)

const heartbeatInterval = 20 * time.Second

func prepareSSE(w http.ResponseWriter) (http.Flusher, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return flusher, true
}

func sseEvent(w http.ResponseWriter, event string, payload []byte) {
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	for _, line := range strings.Split(string(payload), "\n") {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprint(w, "\n")
}

func (s *Server) handleEventStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := prepareSSE(w)
	if !ok {
		s.writeError(w, r, http.StatusInternalServerError, codeInternal, "streaming is not supported")
		return
	}
	events, cancel := s.engine.Bus().Subscribe(256)
	defer cancel()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case ev, open := <-events:
			if !open {
				return
			}
			switch ev.Type {
			case engine.TopicRunUpdated, engine.TopicJobUpdated, engine.TopicArtifactUpdated:
			default:
				continue
			}
			payload, err := json.Marshal(ev.Data)
			if err != nil {
				continue
			}
			sseEvent(w, ev.Type, payload)
			flusher.Flush()
		}
	}
}

func (s *Server) handleRunLogStream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runID := chi.URLParam(r, "id")
	run, err := s.store.Runs.Get(ctx, runID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	flusher, ok := prepareSSE(w)
	if !ok {
		s.writeError(w, r, http.StatusInternalServerError, codeInternal, "streaming is not supported")
		return
	}

	var seq int64
	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()
	poll := time.NewTicker(400 * time.Millisecond)
	defer poll.Stop()

	emit := func() error {
		chunks, err := s.store.Runs.LogChunks(ctx, runID, seq)
		if err != nil {
			return err
		}
		for _, chunk := range chunks {
			seq = chunk.Seq
			for _, line := range strings.Split(strings.TrimRight(chunk.Text, "\n"), "\n") {
				if line == "" {
					continue
				}
				sseEvent(w, "line", []byte(line))
			}
		}
		if len(chunks) > 0 {
			flusher.Flush()
		}
		return nil
	}

	if err := emit(); err != nil {
		return
	}
	if run.Status.Terminal() {
		sseEvent(w, "done", []byte(string(run.Status)))
		flusher.Flush()
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-poll.C:
			if err := emit(); err != nil {
				return
			}
			current, err := s.store.Runs.Get(ctx, runID)
			if err != nil {
				return
			}
			if current.Status.Terminal() {
				if err := emit(); err != nil {
					return
				}
				sseEvent(w, "done", []byte(string(current.Status)))
				flusher.Flush()
				return
			}
		}
	}
}
