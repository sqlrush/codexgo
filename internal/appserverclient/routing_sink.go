package appserverclient

import (
	"sync"

	"github.com/sqlrush/codexgo/internal/appserver"
	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// routingSink is the [appserver.OutgoingSink] the in-process client installs on
// its processor. It routes responses and errors back to the pending request that
// issued them (correlated by integer JSON-RPC id) and routes notifications (and
// unmatched errors) to the event channel.
//
// It is safe for concurrent use: the event forwarder goroutine and request
// dispatch goroutines may both Send concurrently.
type routingSink struct {
	client    *InProcessAppServerClient
	connState *appserver.Conn

	events chan ServerEvent

	mu      sync.Mutex
	closed  bool
	dropped int
}

// compile-time assertion that routingSink satisfies appserver.OutgoingSink.
var _ appserver.OutgoingSink = (*routingSink)(nil)

// newRoutingSink builds a routing sink for client with an event buffer of size.
func newRoutingSink(client *InProcessAppServerClient, buffer int) *routingSink {
	return &routingSink{
		client:    client,
		connState: appserver.NewConn(),
		events:    make(chan ServerEvent, buffer),
	}
}

// Send routes one server-to-client message. Responses and errors with a known
// integer id are delivered to the matching pending request; notifications and
// unmatched errors are pushed onto the event channel (dropped, with a recorded
// count, when the channel is full).
func (s *routingSink) Send(msg appserverproto.JSONRPCMessage) error {
	switch msg.Kind {
	case appserverproto.MessageKindResponse:
		if id, ok := msg.Response.ID.Integer(); ok {
			s.client.deliverResponse(id, RequestResult{Result: msg.Response.Result})
			return nil
		}
		// A string id is not produced by this client; ignore.
		return nil
	case appserverproto.MessageKindError:
		if id, ok := msg.Error.ID.Integer(); ok {
			body := msg.Error.Error
			s.client.deliverResponse(id, RequestResult{Error: &body})
			return nil
		}
		s.pushEvent(ServerEvent{Error: msg.Error})
		return nil
	case appserverproto.MessageKindNotification:
		s.pushEvent(ServerEvent{Notification: msg.Notification})
		return nil
	default:
		return nil
	}
}

// pushEvent enqueues an event, dropping it (and recording the drop) when the
// channel is full so the engine never blocks on a slow consumer.
func (s *routingSink) pushEvent(ev ServerEvent) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	select {
	case s.events <- ev:
	default:
		s.mu.Lock()
		s.dropped++
		s.mu.Unlock()
	}
}

// close shuts the event channel. Subsequent pushEvent calls are no-ops.
func (s *routingSink) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	close(s.events)
}
