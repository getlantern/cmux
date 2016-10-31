package cmux

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDeadline(t *testing.T) {
	c1 := &cmconn{}
	c2 := &cmconn{}
	now := time.Now()
	t1 := now.Add(8 * time.Second)
	t2 := now.Add(20 * time.Second)
	t3 := now.Add(30 * time.Second)

	var current time.Time
	doSet := func(t time.Time) error {
		current = t
		return nil
	}

	d := newDeadline(10*time.Second, doSet)
	d.set(c1, t2)
	assert.Equal(t, t2, current, "First deadline should have been set")
	d.set(c2, t3)
	assert.Equal(t, t2, current, "Longer deadline should not have been set")
	d.set(c2, t1)
	assert.True(t, current.Before(t2), "Earlier deadline should have been set")
	d.set(c2, t3)
	assert.Equal(t, t3, current, "Later deadline for same connection should have been set")
}
