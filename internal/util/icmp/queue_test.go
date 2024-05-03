package icmp

import (
	"fmt"
	"testing"
	"time"
)

func TestQueue(t *testing.T) {
	q := newQueue[int](4)
	for i := 0; i < 10; i++ {
		q.Write(i)
	}
	for i := 0; i < 2; i++ {
		fmt.Println(q.buf, q.writeCursor, q.readCursor)
		fmt.Println(q.Read())
	}
	for i := 10; i < 20; i++ {
		q.Write(i)
	}
	go func() {
		time.Sleep(time.Millisecond * 1000)
		q.Write(20)
		q.Write(21)
		q.Write(22)
	}()
	go func() {
		for {
			i := 1
			i++
		}
	}()
	for i := 0; i < 6; i++ {
		fmt.Println(q.buf, q.writeCursor, q.readCursor)
		fmt.Println(q.Read())
	}
}
