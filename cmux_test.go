package cmux

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getlantern/fdcount"
	"github.com/getlantern/keyman"
	"github.com/stretchr/testify/assert"
)

func TestRoundTrip(t *testing.T) {
	_, fdc, err := fdcount.Matching("TCP")
	if err != nil {
		t.Fatal(err)
	}

	pk, err := keyman.GeneratePK(2048)
	if !assert.NoError(t, err) {
		return
	}
	cert, err := pk.TLSCertificateFor(time.Now().Add(365*24*time.Hour), true, nil, "cmux", "127.0.0.1")
	keypair, err := tls.X509KeyPair(cert.PEMEncoded(), pk.PEMEncoded())

	_l, err := tls.Listen("tcp", "localhost:0", &tls.Config{
		Certificates: []tls.Certificate{keypair},
	})
	if !assert.NoError(t, err) {
		return
	}

	l := Listen(&ListenOpts{Listener: _l}).(*listener)
	assert.NoError(t, fdc.AssertDelta(1), "Starting listener should add only 1 file descriptor")

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		for {
			conn, acceptErr := l.Accept()
			if acceptErr != nil {
				log.Error(acceptErr)
				return
			}
			// Start echoing
			go func() {
				io.Copy(conn, conn)
				conn.Close()
			}()
		}
	}()

	defer func() {
		l.Close()
		// Make sure we can close twice without problem
		l.Close()
		wg.Wait()
		assert.NoError(t, fdc.AssertDelta(0), "After closing listener, there should be no lingering file descriptors")
	}()

	dial := Dialer(&DialerOpts{Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
		return tls.Dial(network, addr, &tls.Config{
			RootCAs: cert.PoolContainingCert(),
		})
	}, PoolSize: 2})

	log.Debug("1")
	c1, err := dial(context.Background(), "tcp", l.Addr().String())
	if !assert.NoError(t, err) {
		return
	}
	defer c1.Close()
	log.Debug("2")
	assert.NoError(t, fdc.AssertDelta(3), "Dialing connection 1 should have added one underlying connection (one file descriptor for each end of connection)")
	assert.EqualValues(t, 1, atomic.LoadInt64(&l.numConnections))
	assert.EqualValues(t, 1, atomic.LoadInt64(&l.numVirtualConnections))

	c2, err := dial(context.Background(), "tcp", l.Addr().String())
	if !assert.NoError(t, err) {
		return
	}
	defer c2.Close()
	assert.NoError(t, fdc.AssertDelta(5), "Dialing connection 2 should have added another underlying TCP connection")
	assert.EqualValues(t, 2, atomic.LoadInt64(&l.numConnections))
	assert.EqualValues(t, 2, atomic.LoadInt64(&l.numVirtualConnections))

	c3, err := dial(context.Background(), "tcp", l.Addr().String())
	if !assert.NoError(t, err) {
		return
	}
	defer c3.Close()
	assert.NoError(t, fdc.AssertDelta(5), "Dialing connection 3 should not have added any underlying TCP connections")
	assert.EqualValues(t, 2, atomic.LoadInt64(&l.numConnections))
	assert.EqualValues(t, 3, atomic.LoadInt64(&l.numVirtualConnections))

	_, err = c1.Write([]byte("c1"))
	if !assert.NoError(t, err) {
		return
	}
	_, err = c2.Write([]byte("c2"))
	if !assert.NoError(t, err) {
		return
	}
	_, err = c3.Write([]byte("c3"))
	if !assert.NoError(t, err) {
		return
	}

	buf := make([]byte, 2)
	_, err = io.ReadFull(c2, buf)
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, "c2", string(buf))
	_, err = io.ReadFull(c3, buf)
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, "c3", string(buf))
	_, err = io.ReadFull(c1, buf)
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, "c1", string(buf))

	c1.Close()
	assert.NoError(t, fdc.AssertDelta(5), "Closing connection 1 should not have closed any underlying TCP connections")
	assert.EqualValues(t, 2, atomic.LoadInt64(&l.numConnections))
	assert.EqualValues(t, 2, atomic.LoadInt64(&l.numVirtualConnections))

	c3.Close()
	assert.NoError(t, fdc.AssertDelta(3), "Closing connection 3 should have closed one underlying TCP connection")
	assert.EqualValues(t, 1, atomic.LoadInt64(&l.numConnections))
	assert.EqualValues(t, 1, atomic.LoadInt64(&l.numVirtualConnections))

	c2.Close()
	assert.NoError(t, fdc.AssertDelta(1), "Closing connection 2 should have closed remaining underlying TCP connection")
	assert.EqualValues(t, 0, atomic.LoadInt64(&l.numConnections))
	assert.EqualValues(t, 0, atomic.LoadInt64(&l.numVirtualConnections))
}

func TestClose(t *testing.T) {
	_l, err := net.Listen("tcp", "localhost:0")
	if !assert.NoError(t, err) {
		return
	}

	l := Listen(&ListenOpts{Listener: _l})
	_, fdc, err := fdcount.Matching("TCP")
	if err != nil {
		t.Fatal(err)
	}

	// Create a new connnection and close while trying to read and write
	dialer := &net.Dialer{}
	dial := Dialer(&DialerOpts{Dial: dialer.DialContext, PoolSize: 1})
	c, err := dial(context.Background(), "tcp", l.Addr().String())
	if !assert.NoError(t, err) {
		return
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		b := make([]byte, 1024)
		n, readErr := c.Read(b)
		assert.Error(t, readErr)
		assert.Equal(t, 0, n)
		wg.Done()
		log.Debug("Done")
	}()
	time.Sleep(250 * time.Millisecond)
	c.Close()
	log.Debug("Closed")
	wg.Wait()
	assert.NoError(t, fdc.AssertDelta(0), "No connections should remain open after closing connection")

	b := make([]byte, 1024)
	n, writeErr := c.Write(b)
	assert.Error(t, writeErr)
	assert.Equal(t, 0, n)
}
