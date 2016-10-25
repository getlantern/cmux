package cmux

import (
	"github.com/getlantern/fdcount"
	"github.com/stretchr/testify/assert"
	"io"
	"net"
	"sync"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	_, fdc, err := fdcount.Matching("TCP")
	if err != nil {
		t.Fatal(err)
	}

	_l, err := net.Listen("tcp", "localhost:0")
	if !assert.NoError(t, err) {
		return
	}

	l := Listen(&ListenOpts{Listener: _l})
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
			defer conn.Close()
			// Start echoing
			go io.Copy(conn, conn)
		}
	}()

	defer func() {
		l.Close()
		// Make sure we can close twice without problem
		l.Close()
		wg.Wait()
		assert.NoError(t, fdc.AssertDelta(0), "After closing listener, there should be no lingering file descriptors")
	}()

	dial := Dialer(&DialerOpts{Dial: net.Dial, PoolSize: 2})

	c1, err := dial("tcp", l.Addr().String())
	if !assert.NoError(t, err) {
		return
	}
	defer c1.Close()
	assert.NoError(t, fdc.AssertDelta(3), "Dialing connection 1 should have added one underlying connection (one file descriptor for each end of connection)")

	c2, err := dial("tcp", l.Addr().String())
	if !assert.NoError(t, err) {
		return
	}
	defer c2.Close()
	assert.NoError(t, fdc.AssertDelta(5), "Dialing connection 2 should have added another underlying TCP connection")

	c3, err := dial("tcp", l.Addr().String())
	if !assert.NoError(t, err) {
		return
	}
	defer c3.Close()
	assert.NoError(t, fdc.AssertDelta(5), "Dialing connection 3 should not have added any underlying TCP connections")

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
	c3.Close()
	assert.NoError(t, fdc.AssertDelta(3), "Closing connection 3 should have closed one underlying TCP connection")
	c2.Close()
	assert.NoError(t, fdc.AssertDelta(1), "Closing connection 2 should have closed remaining underlying TCP connection")
}
