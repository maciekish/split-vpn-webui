package server

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type streamMessage struct {
	Event string
	Data  []byte
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

	ch := make(chan streamMessage, 16)
	s.addWatcher(ch)
	defer s.removeWatcher(ch)

	release := s.latency.Activate()
	defer release()

	ctx := r.Context()
	fmt.Fprintf(w, "retry: 5000\n\n")
	flusher.Flush()

	initial := s.createPayload(nil)
	bytes, _ := json.Marshal(initial)
	fmt.Fprintf(w, "data: %s\n\n", bytes)
	flusher.Flush()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if len(msg.Data) == 0 {
				continue
			}
			if msg.Event != "" {
				fmt.Fprintf(w, "event: %s\n", msg.Event)
			}
			fmt.Fprintf(w, "data: %s\n\n", msg.Data)
			flusher.Flush()
		}
	}
}

func (s *Server) addWatcher(ch chan streamMessage) {
	s.watchersMu.Lock()
	defer s.watchersMu.Unlock()
	s.watchers[ch] = struct{}{}
}

func (s *Server) removeWatcher(ch chan streamMessage) {
	s.watchersMu.Lock()
	defer s.watchersMu.Unlock()
	if _, ok := s.watchers[ch]; ok {
		delete(s.watchers, ch)
		close(ch)
	}
}

func (s *Server) broadcastUpdate(errMap map[string]string) {
	s.watchersMu.Lock()
	watchers := make([]chan streamMessage, 0, len(s.watchers))
	for ch := range s.watchers {
		watchers = append(watchers, ch)
	}
	s.watchersMu.Unlock()
	if len(watchers) == 0 {
		return
	}
	payload := s.createPayload(errMap)
	bytes, err := json.Marshal(payload)
	if err != nil {
		return
	}
	msg := streamMessage{Data: bytes}
	for _, ch := range watchers {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (s *Server) broadcastEvent(event string, payload any) {
	s.watchersMu.Lock()
	watchers := make([]chan streamMessage, 0, len(s.watchers))
	for ch := range s.watchers {
		watchers = append(watchers, ch)
	}
	s.watchersMu.Unlock()
	if len(watchers) == 0 {
		return
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return
	}
	msg := streamMessage{Event: event, Data: bytes}
	for _, ch := range watchers {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (s *Server) createPayload(errMap map[string]string) UpdatePayload {
	snapshot := s.stats.Snapshot()
	results := s.latency.Results()
	_, statuses, configErrors := s.collectConfigStatuses()
	if errMap == nil {
		errMap = make(map[string]string)
	}
	for k, v := range configErrors {
		errMap[k] = v
	}
	return UpdatePayload{
		Stats:   snapshot,
		Latency: results,
		Configs: statuses,
		Errors:  errMap,
	}
}
