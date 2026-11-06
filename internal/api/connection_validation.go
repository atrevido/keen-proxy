package api

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/atrevido/keenetic-proxy/internal/compile"
	"github.com/atrevido/keenetic-proxy/internal/engine"
	"github.com/atrevido/keenetic-proxy/internal/model"
	"regexp"
)

// validateEngineProfiles checks profile fields and constructs engine options without starting it.
// Returned errors deliberately omit raw parser details that may contain credentials.
func validateEngineProfiles(ctx context.Context, cfg *model.Config) error {
	if err := validateProfileFields(cfg); err != nil {
		return err
	}
	raw, err := compile.ConnectionValidationConfig(cfg)
	if err == nil {
		err = engine.Validate(ctx, raw)
	}
	// Parser/constructor errors may contain passwords, UUIDs or keys.
	if err != nil {
		return errors.New("параметры соединения не приняты ядром: проверьте тип, обязательные поля и ключи")
	}
	return nil
}

var profileUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)

var serverProfileTypes = map[string]bool{"vless": true, "trojan": true, "shadowsocks": true, "hysteria2": true, "socks": true}
var passwordProfileTypes = map[string]bool{"trojan": true, "hysteria2": true, "shadowsocks": true}

func invalidProfileField(field string) error {
	return fmt.Errorf("неверный формат поля соединения: %s", field)
}

// validWGKey requires a base64-encoded 32-byte WireGuard key.
func validWGKey(value string) bool {
	raw, err := base64.StdEncoding.DecodeString(value)
	return err == nil && len(raw) == 32
}
func validateWGKeys(wg *model.WireGuard) error {
	if wg == nil {
		return nil
	}
	if !validWGKey(wg.PrivateKey) || !validWGKey(wg.Peer.PublicKey) {
		return invalidProfileField("WireGuard key")
	}
	if wg.Peer.PresharedKey != "" && !validWGKey(wg.Peer.PresharedKey) {
		return invalidProfileField("WireGuard key")
	}
	return nil
}
func profileString(out map[string]any, key string) string {
	value, _ := out[key].(string)
	return value
}

// validateOutboundFields enforces required fields that permissive engine decoding may accept.
func validateOutboundFields(out map[string]any) error {
	typ := profileString(out, "type")
	if serverProfileTypes[typ] {
		if profileString(out, "server") == "" {
			return invalidProfileField("server")
		}
		if out["server_port"] == nil {
			return invalidProfileField("server_port")
		}
	}
	if typ == "vless" && !profileUUID.MatchString(profileString(out, "uuid")) {
		return invalidProfileField("uuid")
	}
	if passwordProfileTypes[typ] && profileString(out, "password") == "" {
		return invalidProfileField("password")
	}
	return nil
}
func validateProfileFields(cfg *model.Config) error {
	for _, connection := range cfg.Connections {
		if err := validateWGKeys(connection.WireGuard); err != nil {
			return err
		}
		if err := validateOutboundFields(connection.Outbound); err != nil {
			return err
		}
	}
	return nil
}
