package engine

import (
	"context"
	"testing"
)

func TestValidateSchemaWithoutStartingEngine(t *testing.T) {
	if err := Validate(context.Background(), []byte(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{`, `{"outbounds":[{"type":"unknown"}]}`, `{"outbounds":[{"type":"shadowsocks","server":"203.0.113.1","server_port":443,"method":"invalid-method","password":"example"}]}`} {
		if err := Validate(context.Background(), []byte(raw)); err == nil {
			t.Fatalf("accepted invalid schema: %s", raw)
		}
	}
}
