package cmux

import (
	"sync"
	"time"
)

var (
	maxDeadline = time.Unix(1<<63-62135596801, 999999999)
)

// deadline is a wrapper around connection deadlines that updates the deadline
// if the logical connection was the last to set the deadline or if the new
// deadline is less than the current deadline. This allows us to manage
// deadlines on underlying connections even multiplexing virtual connections
// over them.
type deadline struct {
	current time.Time
	owner   *cmconn
	doSet   func(time.Time) error
	mx      sync.Mutex
}

func newDeadline(doSet func(time.Time) error) *deadline {
	return &deadline{
		current: maxDeadline,
		owner:   nil,
		doSet:   doSet,
	}
}

func (d *deadline) set(by *cmconn, t time.Time) (err error) {
	d.mx.Lock()
	if by == d.owner || t.Before(d.current) {
		err = d.doSet(t)
		d.current = t
		d.owner = by
	}
	d.mx.Unlock()
	return
}
