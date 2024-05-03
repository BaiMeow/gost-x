package quic

import (
	"time"

	mdata "github.com/go-gost/core/metadata"
	mdutil "github.com/go-gost/core/metadata/util"
)

const (
	defaultMinAirSeq = 4
	defaultMaxAirSeq = 128
)

type metadata struct {
	keepAlivePeriod  time.Duration
	maxIdleTimeout   time.Duration
	handshakeTimeout time.Duration
	seqBySeqMode     bool
	minAirSeq        int
	maxAirSeq        int
}

func (d *icmpDialer) parseMetadata(md mdata.Metadata) (err error) {
	const (
		keepAlive        = "keepAlive"
		keepAlivePeriod  = "ttl"
		handshakeTimeout = "handshakeTimeout"
		maxIdleTimeout   = "maxIdleTimeout"
		seqBySeqMode     = "seqBySeqMode"
		minAirSeq        = "minAirSeq"
		maxAirSeq        = "maxAirSeq"
	)

	if mdutil.GetBool(md, keepAlive) {
		d.md.keepAlivePeriod = mdutil.GetDuration(md, keepAlivePeriod)
		if d.md.keepAlivePeriod <= 0 {
			d.md.keepAlivePeriod = 10 * time.Second
		}
	}
	d.md.handshakeTimeout = mdutil.GetDuration(md, handshakeTimeout)
	d.md.maxIdleTimeout = mdutil.GetDuration(md, maxIdleTimeout)

	d.md.seqBySeqMode = mdutil.GetBool(md, seqBySeqMode)
	d.md.minAirSeq = mdutil.GetInt(md, minAirSeq)
	if d.md.minAirSeq <= 0 {
		d.md.minAirSeq = defaultMinAirSeq
	}
	d.md.maxAirSeq = mdutil.GetInt(md, maxAirSeq)
	if d.md.maxAirSeq <= 0 {
		d.md.maxAirSeq = defaultMaxAirSeq
	}
	return
}
