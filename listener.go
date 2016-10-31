package cmux

import (
	"github.com/xtaci/smux"
	"net"
	"sync"
)

type ListenOpts struct {
	Listener   net.Listener
	BufferSize int
}

type listener struct {
	wrapped       net.Listener
	bufferSize    int
	nextConn      chan net.Conn
	nextErr       chan error
	sessions      map[int]*smux.Session
	nextSessionID int
	closed        bool
	mx            sync.Mutex
}

// Listen creates a net.Listener that multiplexes connections over a connection
// obtained from the underlying opts.Listener.
func Listen(opts *ListenOpts) net.Listener {
	if opts.BufferSize <= 0 {
		opts.BufferSize = defaultBufferSize
	}
	l := &listener{
		wrapped:    opts.Listener,
		bufferSize: opts.BufferSize,
		nextConn:   make(chan net.Conn, 1),
		nextErr:    make(chan error, 1),
		sessions:   make(map[int]*smux.Session),
	}
	go l.listen()
	return l
}

func (l *listener) listen() {
	defer l.Close()
	for {
		conn, err := l.wrapped.Accept()
		if err != nil {
			l.nextErr <- err
			return
		}
		go l.handleConn(conn)
	}
}

func (l *listener) handleConn(conn net.Conn) {
	readDeadline := newDeadline(conn.SetReadDeadline)
	writeDeadline := newDeadline(conn.SetWriteDeadline)

	l.mx.Lock()
	smuxConfig := smux.DefaultConfig()
	smuxConfig.MaxReceiveBuffer = l.bufferSize
	session, err := smux.Server(conn, smuxConfig)
	if err != nil {
		l.nextErr <- err
		l.mx.Unlock()
		return
	}
	sessionID := l.nextSessionID
	l.nextSessionID++
	l.sessions[sessionID] = session
	l.mx.Unlock()

	defer func() {
		l.mx.Lock()
		if !l.closed {
			session.Close()
			delete(l.sessions, sessionID)
		}
		l.mx.Unlock()
	}()

	for {
		stream, err := session.AcceptStream()
		if err != nil {
			log.Errorf("Error creating multiplexed session: %v", err)
			return
		}
		l.nextConn <- newConn(conn, readDeadline, writeDeadline, stream, nil)
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
	l.mx.Lock()
	if l.closed {
		l.mx.Unlock()
		return nil
	}
	for _, session := range l.sessions {
		session.Close()
	}
	l.sessions = nil
	err := l.wrapped.Close()
	l.closed = true
	l.mx.Unlock()
	return err
}

func (l *listener) Addr() net.Addr {
	return l.wrapped.Addr()
}
