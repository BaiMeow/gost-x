package icmp

import (
	"context"
	"fmt"
	"github.com/go-gost/core/common/bufpool"
	"github.com/go-gost/core/logger"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	BufQueueLen = 1024
	BoostPeriod = 500 * time.Millisecond
)

type clientConn2 struct {
	net.PacketConn
	raddr    *net.UDPAddr
	bufQueue chan []byte
	// seq is uint16 but only uint32 has atomic operation
	seq     uint32
	peerSeq uint32
	cancel  context.CancelFunc
	ctx     context.Context

	airSeqCount    int
	minAirSeqCount int
	maxAirSeqCount int

	seqTicker  *time.Ticker
	readCouter atomic.Uint32
}

func (c *clientConn2) LocalAddr() net.Addr {
	return c.raddr
}

func ClientConn2(conn net.PacketConn, raddr *net.UDPAddr, minAirSeqCount int, maxAirSeqCount int) net.PacketConn {
	ctx, cancel := context.WithCancel(context.Background())
	c := &clientConn2{
		PacketConn:     conn,
		raddr:          raddr,
		cancel:         cancel,
		bufQueue:       make(chan []byte, BufQueueLen),
		ctx:            ctx,
		airSeqCount:    minAirSeqCount,
		minAirSeqCount: minAirSeqCount,
		maxAirSeqCount: maxAirSeqCount,
		seqTicker:      time.NewTicker(BoostPeriod / time.Duration(minAirSeqCount)),
	}
	go c.SeqKeeper()
	go c.Booster()
	return c
}

func (c *clientConn2) Close() error {
	c.cancel()
	return c.PacketConn.Close()
}

func (c *clientConn2) SeqKeeper() {
	for {
		select {
		case <-c.ctx.Done():
			c.seqTicker.Stop()
			return
		case <-c.seqTicker.C:
			if _, err := c.WriteTo(nil, c.raddr); err != nil {
				logger.Default().Error(err)
				return
			}
		}
	}

}

func (c *clientConn2) Booster() {
	tBoost := time.NewTicker(BoostPeriod)
	for {
		select {
		case <-c.ctx.Done():
			tBoost.Stop()
			return
		case <-tBoost.C:
			count := c.readCouter.Swap(0)
			// ensure fold [0,1]
			fold := min(float64(count)/float64(c.airSeqCount), 1)
			calc := math.Round((math.Pow(fold, 1.6)-0.5)*float64(c.airSeqCount) + float64(c.airSeqCount))
			calc = min(max(calc, float64(c.minAirSeqCount)), float64(c.maxAirSeqCount))
			logger.Default().Debugf("icmp: air seq count %d", int(calc))
			c.airSeqCount = int(calc)
			c.cancelSeqOnce()
		}
	}

}

func (c *clientConn2) cancelSeqOnce() {
	c.seqTicker.Reset(BoostPeriod / time.Duration(c.airSeqCount))
}

func (c *clientConn2) ReadFrom(b []byte) (n int, addr net.Addr, err error) {
	buf := bufpool.Get(readBufferSize)
	defer bufpool.Put(buf)

	for {
		n, addr, err = c.PacketConn.ReadFrom(buf)
		if err != nil {
			c.Close()
			return
		}

		m, err := icmp.ParseMessage(1, buf[:n])
		if err != nil {
			// logger.Default().Error("icmp: parse message %v", err)
			return 0, addr, err
		}
		echo, ok := m.Body.(*icmp.Echo)
		if !ok || m.Type != ipv4.ICMPTypeEchoReply {
			// logger.Default().Warnf("icmp: invalid type %s (discarded)", m.Type)
			continue // discard
		}
		if echo.ID != c.raddr.Port {
			// logger.Default().Warnf("icmp: id mismatch got %d, should be %d (discarded)", echo.ID, c.id)
			continue
		}

		c.readCouter.Add(1)

		msg := message{}
		if _, err := msg.Decode(echo.Data); err != nil {
			logger.Default().Warn(err)
			continue
		}

		if msg.flags&FlagAck == 0 {
			// logger.Default().Warn("icmp: invalid message (discarded)")
			continue
		}

		if msg.flags&FlagKeepAlive > 0 {
			continue
		}

		n = copy(b, msg.data)
		break
	}

	if v, ok := addr.(*net.IPAddr); ok {
		addr = &net.UDPAddr{
			IP:   v.IP,
			Port: c.raddr.Port,
		}
	}
	// logger.Default().Infof("icmp: read from: %v %d", addr, n)

	return
}

func (c *clientConn2) WriteTo(b []byte, addr net.Addr) (n int, err error) {
	// logger.Default().Infof("icmp: write to: %v %d", addr, len(b))
	switch v := addr.(type) {
	case *net.UDPAddr:
		addr = &net.IPAddr{IP: v.IP}
	}

	buf := bufpool.Get(writeBufferSize)
	defer bufpool.Put(buf)

	msg := message{
		data: b,
	}
	if len(b) == 0 {
		msg.flags |= FlagKeepAlive
	}
	nn, err := msg.Encode(buf)
	if err != nil {
		return
	}

	echo := icmp.Echo{
		ID:   c.raddr.Port,
		Seq:  int(uint16(atomic.AddUint32(&c.seq, 1))),
		Data: buf[:nn],
	}
	m := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &echo,
	}
	wb, err := m.Marshal(nil)
	if err != nil {
		return 0, err
	}
	c.cancelSeqOnce()
	_, err = c.PacketConn.WriteTo(wb, addr)
	n = len(b)
	return
}

type client struct {
	id   uint16
	seqC *queue[uint16]
}

type serverConn2 struct {
	net.PacketConn
	// it's bad to create 65535 channels, so just turns to use sync.Map
	clients      sync.Map
	seqQueueSize int
}

func ServerConn2(conn net.PacketConn, seqQueueSize int) net.PacketConn {
	return &serverConn2{
		PacketConn:   conn,
		seqQueueSize: seqQueueSize,
	}
}

func (c *serverConn2) ReadFrom(b []byte) (n int, addr net.Addr, err error) {
	buf := bufpool.Get(readBufferSize)
	defer bufpool.Put(buf)

	for {
		n, addr, err = c.PacketConn.ReadFrom(buf)
		if err != nil {
			return
		}

		m, err := icmp.ParseMessage(1, buf[:n])
		if err != nil {
			// logger.Default().Error("icmp: parse message %v", err)
			return 0, addr, err
		}

		echo, ok := m.Body.(*icmp.Echo)
		if !ok || m.Type != ipv4.ICMPTypeEcho || echo.ID <= 0 {
			// logger.Default().Warnf("icmp: invalid type %s (discarded)", m.Type)
			continue
		}

		cl, ok := c.clients.Load(echo.ID)
		if !ok {
			cl, _ = c.clients.LoadOrStore(echo.ID, &client{id: uint16(echo.ID), seqC: newQueue[uint16](c.seqQueueSize)})
		}

		cl.(*client).seqC.Write(uint16(echo.Seq))

		msg := message{}
		if _, err := msg.Decode(echo.Data); err != nil {
			continue
		}

		if msg.flags&FlagKeepAlive > 0 {
			continue
		}

		if msg.flags&FlagAck > 0 {
			continue
		}

		n = copy(b, msg.data)

		if v, ok := addr.(*net.IPAddr); ok {
			addr = &net.UDPAddr{
				IP:   v.IP,
				Port: echo.ID,
			}
		}
		break
	}

	// logger.Default().Infof("icmp: read from: %v %d", addr, n)

	return
}

func (c *serverConn2) WriteTo(b []byte, addr net.Addr) (n int, err error) {
	// logger.Default().Infof("icmp: write to: %v %d", addr, len(b))
	var id int
	switch v := addr.(type) {
	case *net.UDPAddr:
		addr = &net.IPAddr{IP: v.IP}
		id = v.Port
	}

	if id <= 0 || id > math.MaxUint16 {
		err = fmt.Errorf("icmp: invalid message id %v", addr)
		return
	}

	buf := bufpool.Get(writeBufferSize)
	defer bufpool.Put(buf)

	msg := message{
		flags: FlagAck,
		data:  b,
	}
	nn, err := msg.Encode(buf)
	if err != nil {
		return
	}

	cl, ok := c.clients.Load(id)
	if !ok {
		err = fmt.Errorf("icmp: invalid message id %v", addr)
		return
	}
	echo := icmp.Echo{
		ID:   id,
		Seq:  int(cl.(*client).seqC.Read()),
		Data: buf[:nn],
	}
	m := icmp.Message{
		Type: ipv4.ICMPTypeEchoReply,
		Code: 0,
		Body: &echo,
	}
	wb, err := m.Marshal(nil)
	if err != nil {
		return 0, err
	}
	_, err = c.PacketConn.WriteTo(wb, addr)
	n = len(b)
	return
}
