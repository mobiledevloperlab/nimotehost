// SPDX-License-Identifier: Apache-2.0
package host

import (
	"net"
	"sync"
)

// Entries exist only while a device has admitted work. Neither stale identities
// nor unbounded attacker-chosen keys accumulate in this map.
type principalUsage struct{ reads, writes int }
type requestAdmission struct {
	mu                      sync.Mutex
	devices                 map[string]principalUsage
	ownerReads, ownerWrites chan struct{}
}

func newRequestAdmission() *requestAdmission {
	return &requestAdmission{devices: make(map[string]principalUsage), ownerReads: make(chan struct{}, 8), ownerWrites: make(chan struct{}, 4)}
}
func (a *requestAdmission) acquire(id string, owner, mutation bool) (func(), bool) {
	if owner {
		slots := a.ownerReads
		if mutation {
			slots = a.ownerWrites
		}
		select {
		case slots <- struct{}{}:
			return func() { <-slots }, true
		default:
			return nil, false
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	n := a.devices[id]
	if (!mutation && n.reads >= 8) || (mutation && n.writes >= 2) {
		return nil, false
	}
	if mutation {
		n.writes++
	} else {
		n.reads++
	}
	a.devices[id] = n
	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		n := a.devices[id]
		if mutation {
			n.writes--
		} else {
			n.reads--
		}
		if n.reads == 0 && n.writes == 0 {
			delete(a.devices, id)
		} else {
			a.devices[id] = n
		}
	}, true
}

// Limits accepted sockets as well as handlers; idle unauthenticated connections
// are covered by the existing HTTP header/idle deadlines. Close unblocks Accept
// both while waiting for capacity and while inside the underlying listener.
type boundedListener struct {
	net.Listener
	slots chan struct{}
	done  chan struct{}
	once  sync.Once
}

func newBoundedListener(listener net.Listener, limit int) *boundedListener {
	return &boundedListener{Listener: listener, slots: make(chan struct{}, limit), done: make(chan struct{})}
}
func (l *boundedListener) Accept() (net.Conn, error) {
	select {
	case l.slots <- struct{}{}:
	case <-l.done:
		return nil, net.ErrClosed
	}
	conn, err := l.Listener.Accept()
	if err != nil {
		<-l.slots
		return nil, err
	}
	return &boundedConn{Conn: conn, release: func() { <-l.slots }}, nil
}
func (l *boundedListener) Close() error {
	var err error
	l.once.Do(func() { close(l.done); err = l.Listener.Close() })
	return err
}

type boundedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *boundedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}
