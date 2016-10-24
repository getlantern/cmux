package cmux

import (
	"github.com/xtaci/smux"
	"net"
)

type ListenOpts struct {
	Listener   net.Listener
	BufferSize int
}

func Listen(opts *ListenOpts) net.Listener {
	if opts.BufferSize <= 0 {
		opts.BufferSize = 4194304
	}
	l := &listener{
		wrapped:    opts.Listener,
		bufferSize: opts.BufferSize,
		nextConn:   make(chan net.Conn),
		nextErr:    make(chan error),
	}
	go l.listen()
	return l
}

type listener struct {
	wrapped    net.Listener
	bufferSize int
	nextConn   chan net.Conn
	nextErr    chan error
}

func (l *listener) listen() {
	defer l.Close()
	for {
		conn, err := l.wrapped.Accept()
		if err != nil {
			l.nextErr <- err
		}
		go l.handleConn(conn)
	}
}

func (l *listener) handleConn(conn net.Conn) {
	smuxConfig := smux.DefaultConfig()
	smuxConfig.MaxReceiveBuffer = l.bufferSize
	session, err := smux.Server(conn, smuxConfig)
	if err != nil {
		l.nextErr <- err
		return
	}
	defer session.Close()
	for {
		stream, err := session.AcceptStream()
		if err != nil {
			log.Errorf("Error creating multiplexed session: %v", err)
			return
		}
		l.nextConn <- &cmconn{conn, stream}
	}
}

func (l *listener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.nextConn:
		return conn, nil
	case err := <-l.nextErr:
		return nil, err
	}
}

func (l *listener) Close() error {
	return l.wrapped.Close()
}

func (l *listener) Addr() net.Addr {
	return l.wrapped.Addr()
}
