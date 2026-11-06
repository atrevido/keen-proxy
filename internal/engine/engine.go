// Package engine manages sing-box inside the keenproxy process.
package engine

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	sjson "github.com/sagernet/sing/common/json"
)

// Engine owns the embedded sing-box instance and serializes lifecycle operations.
type Engine struct {
	// Tracker receives every routed connection for UI statistics.
	Tracker adapter.ConnectionTracker
	// LogOutput redirects sing-box logs from stderr to a file or /dev/fd/N.
	LogOutput string

	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	box     *box.Box
	current []byte
}

// Apply starts sing-box or restarts it when configuration changes. It reports
// whether a new instance was started.
func (e *Engine) Apply(config []byte) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.box != nil && bytes.Equal(e.current, config) {
		return false, nil
	}
	ctx, cancel := context.WithCancel(Context(context.Background()))
	opts, err := sjson.UnmarshalExtendedContext[option.Options](ctx, config)
	if err != nil {
		cancel()
		return false, fmt.Errorf("конфиг sing-box: %w", err)
	}
	if e.LogOutput != "" {
		if opts.Log == nil {
			opts.Log = &option.LogOptions{}
		}
		opts.Log.Output = e.LogOutput
	}
	instance, err := box.New(box.Options{Context: ctx, Options: opts})
	if err != nil {
		cancel()
		return false, fmt.Errorf("создание sing-box: %w", err)
	}
	if e.Tracker != nil {
		instance.Router().AppendTracker(e.Tracker)
	}
	// Close the old instance first because both instances bind the same ports.
	e.closeLocked()
	if err := instance.Start(); err != nil {
		instance.Close()
		cancel()
		return false, fmt.Errorf("запуск sing-box: %w", err)
	}
	e.ctx, e.cancel, e.box, e.current = ctx, cancel, instance, config
	return true, nil
}

// Close stops the current instance and cancels its engine context.
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closeLocked()
}

// closeLocked releases instance resources while the engine mutex is held.
func (e *Engine) closeLocked() error {
	if e.box == nil {
		return nil
	}
	err := e.box.Close()
	e.cancel()
	e.box, e.current = nil, nil
	return err
}

// Outbound returns a running outbound or endpoint by tag, with the engine
// context required to dial through it.
func (e *Engine) Outbound(tag string) (adapter.Outbound, context.Context, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.box == nil {
		return nil, nil, false
	}
	o, ok := e.box.Outbound().Outbound(tag)
	return o, e.ctx, ok
}

// Select switches group to outbound tag. Existing selector connections close
// when interrupt_exist_connections is enabled in configuration.
func (e *Engine) Select(group, tag string) bool {
	o, _, ok := e.Outbound(group)
	if !ok {
		return false
	}
	s, ok := o.(interface{ SelectOutbound(string) bool })
	return ok && s.SelectOutbound(tag)
}
