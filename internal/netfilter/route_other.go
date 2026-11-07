//go:build !linux

package netfilter

import "errors"

// TProxyRoute is unsupported outside Linux; non-Linux builds support local
// development and tests rather than router operation.
type TProxyRoute struct {
	Mark  uint32
	Table int
}

var errNotLinux = errors.New("ip rule/route are only available on Linux")

// Apply rejects policy-route installation outside Linux.
func (TProxyRoute) Apply() error { return errNotLinux }

// Remove reports that Linux policy-route cleanup is unavailable on this platform.
func (TProxyRoute) Remove() error { return errNotLinux }
