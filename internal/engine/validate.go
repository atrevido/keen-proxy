package engine

import (
	"context"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/option"
	sjson "github.com/sagernet/sing/common/json"
)

// Validate checks schemas and constructors without starting listeners, polling,
// external clients or network connections. The caller supplies profiles only.
func Validate(ctx context.Context, config []byte) error {
	ctx = Context(ctx)
	options, err := sjson.UnmarshalExtendedContext[option.Options](ctx, config)
	if err != nil {
		return err
	}
	instance, err := box.New(box.Options{Context: ctx, Options: options})
	if err != nil {
		return err
	}
	return instance.Close()
}
