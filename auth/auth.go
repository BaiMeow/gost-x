package auth

import (
	"bufio"
	"context"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/go-gost/core/auth"
	"github.com/go-gost/core/logger"
	"github.com/go-gost/x/internal/loader"
)

type options struct {
	auths       map[string]string
	fileLoader  loader.Loader
	redisLoader loader.Loader
	period      time.Duration
	logger      logger.Logger
}

type Option func(opts *options)

func AuthsPeriodOption(auths map[string]string) Option {
	return func(opts *options) {
		opts.auths = auths
	}
}

func ReloadPeriodOption(period time.Duration) Option {
	return func(opts *options) {
		opts.period = period
	}
}

func FileLoaderOption(fileLoader loader.Loader) Option {
	return func(opts *options) {
		opts.fileLoader = fileLoader
	}
}

func RedisLoaderOption(redisLoader loader.Loader) Option {
	return func(opts *options) {
		opts.redisLoader = redisLoader
	}
}

func LoggerOption(logger logger.Logger) Option {
	return func(opts *options) {
		opts.logger = logger
	}
}

// authenticator is an Authenticator that authenticates client by key-value pairs.
type authenticator struct {
	kvs        map[string]string
	mu         sync.RWMutex
	cancelFunc context.CancelFunc
	options    options
}

// NewAuthenticator creates an Authenticator that authenticates client by pre-defined user mapping.
func NewAuthenticator(opts ...Option) auth.Authenticator {
	var options options
	for _, opt := range opts {
		opt(&options)
	}

	ctx, cancel := context.WithCancel(context.TODO())
	p := &authenticator{
		kvs:        make(map[string]string),
		cancelFunc: cancel,
		options:    options,
	}

	if err := p.reload(ctx); err != nil {
		options.logger.Warnf("reload: %v", err)
	}
	if p.options.period > 0 {
		go p.periodReload(ctx)
	}

	return p
}

// Authenticate checks the validity of the provided user-password pair.
func (p *authenticator) Authenticate(user, password string) bool {
	if p == nil || len(p.kvs) == 0 {
		return true
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	v, ok := p.kvs[user]
	return ok && (v == "" || password == v)
}

func (p *authenticator) periodReload(ctx context.Context) error {
	period := p.options.period
	if period < time.Second {
		period = time.Second
	}
	ticker := time.NewTicker(period)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := p.reload(ctx); err != nil {
				p.options.logger.Warnf("reload: %v", err)
				// return err
			}
			p.options.logger.Debugf("auther reload done")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (p *authenticator) reload(ctx context.Context) error {
	kvs := make(map[string]string)
	for k, v := range p.options.auths {
		kvs[k] = v
	}

	m, err := p.load(ctx)
	if err != nil {
		return err
	}
	for k, v := range m {
		kvs[k] = v
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.kvs = kvs

	return nil
}

func (p *authenticator) load(ctx context.Context) (m map[string]string, err error) {
	m = make(map[string]string)

	if p.options.fileLoader != nil {
		r, er := p.options.fileLoader.Load(ctx)
		if er != nil {
			p.options.logger.Warnf("file loader: %v", er)
		}
		if auths, _ := p.parseAuths(r); auths != nil {
			for k, v := range auths {
				m[k] = v
			}
		}
	}
	if p.options.redisLoader != nil {
		r, er := p.options.redisLoader.Load(ctx)
		if er != nil {
			p.options.logger.Warnf("redis loader: %v", er)
		}
		if auths, _ := p.parseAuths(r); auths != nil {
			for k, v := range auths {
				m[k] = v
			}
		}
	}

	return
}

func (p *authenticator) parseAuths(r io.Reader) (auths map[string]string, err error) {
	if r == nil {
		return
	}

	auths = make(map[string]string)

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if n := strings.IndexByte(line, '#'); n >= 0 {
			line = line[:n]
		}
		sp := strings.SplitN(strings.TrimSpace(line), " ", 2)
		if len(sp) == 1 {
			if k := strings.TrimSpace(sp[0]); k != "" {
				auths[k] = ""
			}
		}
		if len(sp) == 2 {
			if k := strings.TrimSpace(sp[0]); k != "" {
				auths[k] = strings.TrimSpace(sp[1])
			}
		}
	}

	err = scanner.Err()
	return
}

func (p *authenticator) Close() error {
	p.cancelFunc()
	if p.options.fileLoader != nil {
		p.options.fileLoader.Close()
	}
	if p.options.redisLoader != nil {
		p.options.redisLoader.Close()
	}
	return nil
}