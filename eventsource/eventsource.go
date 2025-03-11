// Package eventsource implements a server-sent events client according to the
// HTML specification [1].
//
// [1]: https://html.spec.whatwg.org/multipage/server-sent-events.html
package eventsource

import (
	"bufio"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ReadyState constants
const (
	// CONNECTING means that the client is (re-)connecting to the HTTP endpoint.
	CONNECTING = 0
	// OPEN means that the HTTP connection is established.
	OPEN = 1
	// CLOSED is the state after calling Close().
	CLOSED = 2
)

// DefaultRetry is the default reconnection time in milliseconds. May be overwritten by the server.
const DefaultRetry = 3000

// EventSource roughly implements the HTML EventSource interface.
type EventSource struct {
	// URL is the url the client is connecting to.
	URL string
	// ReadyState is one of CONNECTING, OPEN, or CLOSED.
	ReadyState int

	OnOpen    chan bool
	OnMessage chan Message
	OnError   chan error

	onClose chan bool
}

// New creates an EventSource client.
func New(url string) *EventSource {
	es := &EventSource{
		URL:        url,
		ReadyState: CONNECTING,
		OnOpen:     make(chan bool),
		OnMessage:  make(chan Message),
		OnError:    make(chan error),
		onClose:    make(chan bool),
	}

	go es.receive()
	return es
}

// receive connects to the url and receives messages.
func (es *EventSource) receive() {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DisableCompression = true
	client := &http.Client{Transport: tr}
	lastEventID := ""
	retry := time.Duration(DefaultRetry)
	timeout := false
	bodyEOF := make(chan bool)
	defer close(bodyEOF)

	for {
		es.ReadyState = CONNECTING
		if timeout {
			select {
			case <-es.onClose:
				return
			case <-time.After(time.Duration(retry) * time.Millisecond):
			}
		}
		timeout = true
		req, err := http.NewRequest("GET", es.URL, nil)
		if err != nil {
			es.OnError <- err
			continue
		}
		if lastEventID != "" {
			req.Header.Add("Last-Event-ID", lastEventID)
		}
		req.Header.Add("Accept", "text/event-stream")
		res, err := client.Do(req)
		if err != nil {
			es.OnError <- err
			continue
		}
		if ct, _, err := mime.ParseMediaType(res.Header.Get("Content-Type")); err != nil || ct != "text/event-stream" {
			es.OnError <- fmt.Errorf("The sever returned an invalid Content-Type: %s", ct)
			continue
		}
		es.ReadyState = OPEN
		es.OnOpen <- true

		// Read the body.
		go func() {
			scanner := bufio.NewScanner(res.Body)
			data := ""
			eventType := ""
			for scanner.Scan() {
				line := scanner.Text()
				if line == "" {
					// dispatch event
					if data != "" {
						es.OnMessage <- Message{
							EventType:   eventType,
							Data:        strings.TrimSuffix(data, "\n"),
							LastEventID: lastEventID,
						}
					}
					data = ""
					eventType = ""
				}
				parts := strings.SplitN(line, ":", 2)
				value := ""
				if len(parts) > 1 {
					value = strings.TrimPrefix(parts[1], " ")
				}
				switch parts[0] {
				case "":
					// line starts with colon -> ignore
					continue
				case "event":
					eventType = value
				case "data":
					data += value
					data += "\n"
				case "id":
					lastEventID = value
				case "retry":
					if r, err := strconv.ParseUint(value, 10, 32); err == nil {
						retry = time.Duration(r)
					}
				default:
					// ignore field
				}
			}
			if err := scanner.Err(); err != nil {
				es.OnError <- err
			}
			bodyEOF <- true
		}()

		select {
		case <-es.onClose:
			res.Body.Close()
			return
		case <-bodyEOF:
			res.Body.Close()
		}
	}
}

// Close closes the client.
func (es *EventSource) Close() {
	es.onClose <- true
	es.ReadyState = CLOSED
	close(es.OnOpen)
	close(es.OnMessage)
	close(es.OnError)
	close(es.onClose)
}

// Message is an SSE event.
type Message struct {
	// EventType corresponds to the "event" field.
	EventType string
	// Data corresponds to the "data" field, joined with newlines.
	Data string
	// ID corresponds to the last seen "id" field.
	LastEventID string
}
