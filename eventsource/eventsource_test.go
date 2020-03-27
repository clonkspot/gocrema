package eventsource

import (
	"io"
	"net/http"
	"testing"
	"time"
)

func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func TestSimple(t *testing.T) {
	handler := http.NewServeMux()

	addr := "localhost:63121"
	server := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/event-stream")
		switch r.Header.Get("Last-Event-ID") {
		case "":
			io.WriteString(w, "data: abc\n")
			io.WriteString(w, "id: event1\n")
			io.WriteString(w, ": ignored\n")
			io.WriteString(w, "data:xyz\n")
			io.WriteString(w, "event: hello\n")
			io.WriteString(w, "retry: 1\n")
			io.WriteString(w, "\n")
		case "event1":
			io.WriteString(w, "id:event2\n")
			io.WriteString(w, "data\n")
			io.WriteString(w, "retry: 10000\n")
			io.WriteString(w, "\n")
		}
	})

	go server.ListenAndServe()
	defer server.Close()

	es := New("http://" + addr)

	state := 0
loop:
	for {
		select {
		case <-es.OnOpen:
			switch state {
			case 0:
				state = 1
			case 2:
				state = 3
			default:
				t.Errorf("OnOpen: unexpected state %d", state)
			}
		case msg := <-es.OnMessage:
			switch state {
			case 1:
				if msg.Data != "abc\nxyz" {
					t.Errorf("unexpected data: %s", msg.Data)
				}
				if msg.EventType != "hello" {
					t.Errorf("unexpected type: %s", msg.EventType)
				}
				if msg.LastEventID != "event1" {
					t.Errorf("unexpected id: %s", msg.LastEventID)
				}
				state = 2
			case 3:
				if msg.Data != "" {
					t.Errorf("unexpected data: %s", msg.Data)
				}
				if msg.EventType != "" {
					t.Errorf("unexpected type: %s", msg.EventType)
				}
				if msg.LastEventID != "event2" {
					t.Errorf("unexpected id: %s", msg.LastEventID)
				}
				state = 4
				break loop
			default:
				t.Errorf("OnMessage: unexpected state %d", state)
			}
		case err := <-es.OnError:
			t.Error(err)
			t.FailNow()
		}
	}

	time.Sleep(5 * time.Millisecond)
	if es.ReadyState != CONNECTING {
		t.Errorf("unexpected ReadyState %d (should be CONNECTING %d)", es.ReadyState, CONNECTING)
	}
	es.Close()
	if es.ReadyState != CLOSED {
		t.Errorf("unexpected ReadyState %d (should be CLOSED %d)", es.ReadyState, CLOSED)
	}
}
