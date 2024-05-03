package quic

import (
	"time"

	mdata "github.com/go-gost/core/metadata"
	mdutil "github.com/go-gost/core/metadata/util"
)

const (
	defaultBacklog      = 128
	defaultSeqQueueSize = 8
)

type metadata struct {
	keepAlivePeriod  time.Duration
	handshakeTimeout time.Duration
	maxIdleTimeout   time.Duration

	backlog      int
	seqBySeqMode bool
	seqQueueSize int
}

func (l *icmpListener) parseMetadata(md mdata.Metadata) (err error) {
	const (
		keepAlive        = "keepAlive"
		keepAlivePeriod  = "ttl"
		handshakeTimeout = "handshakeTimeout"
		maxIdleTimeout   = "maxIdleTimeout"

		backlog = "backlog"

		seqBySeqMode = "seqBySeqMode"
		seqQueueSize = "seqQueueSize"
	)

	l.md.backlog = mdutil.GetInt(md, backlog)
	if l.md.backlog <= 0 {
		l.md.backlog = defaultBacklog
	}

	if mdutil.GetBool(md, keepAlive) {
		l.md.keepAlivePeriod = mdutil.GetDuration(md, keepAlivePeriod)
		if l.md.keepAlivePeriod <= 0 {
			l.md.keepAlivePeriod = 10 * time.Second
		}
	}
	l.md.handshakeTimeout = mdutil.GetDuration(md, handshakeTimeout)
	l.md.maxIdleTimeout = mdutil.GetDuration(md, maxIdleTimeout)

	if mdutil.GetBool(md, seqBySeqMode) {
		l.md.seqBySeqMode = true
	}

	l.md.seqQueueSize = mdutil.GetInt(md, seqQueueSize)
	if l.md.seqQueueSize <= 0 {
		l.md.seqQueueSize = defaultSeqQueueSize
	}
	return
}
