package grpc

import (
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"istio.io/istio/pilot/pkg/model"
	"time"
)

// Connection holds information about connected client.
type Connection struct {
	// peerAddr is the address of the client, from network layer.
	peerAddr string

	// Time of connection, for debugging
	connectedAt time.Time

	// conID is the connection conID, used as a key in the connection table.
	// Currently based on the node name and a counter.
	conID string

	// proxy is the client to which this connection is established.
	proxy *model.Proxy

	// Sending on this channel results in a push.
	PushChannel chan struct{}

	// Both ADS and SDS streams implement this interface
	stream DiscoveryStream

	// Original node metadata, to avoid unmarshal/marshal.
	// This is included in internal events.
	node *core.Node

	// initialized channel will be closed when proxy is initialized. Pushes, or anything accessing
	// the proxy, should not be started until this channel is closed.
	initialized chan struct{}

	// stop can be used to end the connection manually via debug endpoints. Only to be used for testing.
	stop chan struct{}

	// reqChan is used to receive discovery requests for this connection.
	reqChan chan *discovery.DiscoveryRequest

	// errorChan is used to process error during discovery request processing.
	errorChan chan error
}

// Send with timeout if configured.
func (conn *Connection) send(res *discovery.DiscoveryResponse) error {
	err := conn.stream.Send(res)
	if err != nil {
		log.Errorf("error while sending response: %v", err)
	}
	return err
}

func newConnection(peerAddr string, stream DiscoveryStream) *Connection {
	return &Connection{
		PushChannel: make(chan struct{}),
		initialized: make(chan struct{}),
		stop:        make(chan struct{}),
		reqChan:     make(chan *discovery.DiscoveryRequest, 1),
		errorChan:   make(chan error, 1),
		peerAddr:    peerAddr,
		connectedAt: time.Now(),
		stream:      stream,
	}
}
