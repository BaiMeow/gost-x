package icmp

import (
	"sync"
)

type queue[T any] struct {
	buf         []T
	len         uint64
	readCursor  uint64
	writeCursor uint64
	lock        sync.Mutex
	wait        chan struct{}
}

// len must be pow of 2
func newQueue[T any](size int) *queue[T] {
	return &queue[T]{
		buf:  make([]T, size),
		len:  uint64(size),
		wait: make(chan struct{}),
	}
}

func (q *queue[T]) Write(v T) {
	q.lock.Lock()
	if q.readCursor%q.len == (q.writeCursor+1)%q.len {
		q.readCursor++
	}
	q.writeCursor++
	q.buf[q.writeCursor%q.len] = v
	select {
	case q.wait <- struct{}{}:
	default:
	}
	q.lock.Unlock()
}

func (q *queue[T]) Read() T {
	q.lock.Lock()
	if q.readCursor%q.len == q.writeCursor%q.len {
		q.lock.Unlock()
		<-q.wait
		q.lock.Lock()
	}
	v := q.buf[q.readCursor%q.len]
	q.readCursor++
	q.lock.Unlock()
	return v
}
