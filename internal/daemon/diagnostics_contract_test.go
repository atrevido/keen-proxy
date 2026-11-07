package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/ordo-systems/keen-proxy/internal/model"
)

func TestDiagnosticProfileValidationPrecedesRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name string
		cfg  *model.Config
		id   string
		want error
	}{
		{"no applied configuration", nil, "unknown", ErrDiagnosticConnection},
		{"unknown profile", &model.Config{Connections: []model.Connection{{ID: "one"}}}, "unknown", ErrDiagnosticConnection},
		{"known profile with cancelled request", &model.Config{Connections: []model.Connection{{ID: "one"}}}, "one", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &Daemon{cfg: tc.cfg}
			_, err := d.DiagnosticChecks(ctx, tc.id)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}
