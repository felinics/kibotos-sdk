package kibotos_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

var nextServer atomic.Uint64

type Server struct {
	URL string

	host     string
	listener *pipeListener
	server   *http.Server
}

func newPipeServer(handler http.Handler) *Server {
	host := fmt.Sprintf("test-%d.invalid", nextServer.Add(1))
	listener := &pipeListener{connections: make(chan net.Conn), done: make(chan struct{})}
	s := &Server{"http://" + host, host, listener, &http.Server{Handler: handler}}
	go func() { _ = s.server.Serve(listener) }()
	return s
}

func (s *Server) Client(others ...*Server) *http.Client {
	servers := map[string]*Server{s.host: s}
	for _, other := range others {
		servers[other.host] = other
	}
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
			host, _, _ := strings.Cut(address, ":")
			server, ok := servers[host]
			if !ok {
				return nil, fmt.Errorf("unexpected test address %q", address)
			}
			client, peer := net.Pipe()
			select {
			case server.listener.connections <- peer:
				return client, nil
			case <-ctx.Done():
				client.Close()
				peer.Close()
				return nil, ctx.Err()
			case <-server.listener.done:
				client.Close()
				peer.Close()
				return nil, net.ErrClosed
			}
		},
	}}
}

func (s *Server) Close() {
	_ = s.listener.Close()
	_ = s.server.Close()
}

type pipeListener struct {
	connections chan net.Conn
	done        chan struct{}
	closeOnce   sync.Once
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connections:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.closeOnce.Do(func() { close(l.done) })
	return nil
}

func (l *pipeListener) Addr() net.Addr { return &net.TCPAddr{} }
