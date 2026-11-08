package amneziawg

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type valueRange struct{ low, high uint64 }

func parseRange(value string, limit uint64) (valueRange, bool) {
	low, high, hasRange := strings.Cut(value, "-")
	if !hasRange {
		high = low
	}
	lo, loErr := strconv.ParseUint(low, 10, 32)
	hi, hiErr := strconv.ParseUint(high, 10, 32)
	return valueRange{lo, hi}, loErr == nil && hiErr == nil && lo <= hi && hi <= limit
}

func invalidOption(name string) error {
	return fmt.Errorf("invalid AmneziaWG %s", name)
}

func validateOptions(intf, peer map[string]string) error {
	if err := validatePadding(intf); err != nil {
		return err
	}
	if err := validateHeaders(intf); err != nil {
		return err
	}
	if err := validateSignatures(intf); err != nil {
		return err
	}
	if err := validateTimings(intf, peer); err != nil {
		return err
	}
	return normalizeFlags(intf)
}

func validatePadding(intf map[string]string) error {
	const maxPadding = 4096
	padding := map[string]uint64{
		"ListenPort": 65535, "Jc": 128, "Jmin": maxPadding, "Jmax": maxPadding,
		"S1": maxPadding, "S2": maxPadding, "S3": maxPadding, "S4": maxPadding,
	}
	numbers := make(map[string]uint64, len(padding))
	for name, max := range padding {
		if value := intf[name]; value != "" {
			num, err := strconv.ParseUint(value, 10, 16)
			if err != nil || num > max {
				return invalidOption(name)
			}
			numbers[name] = num
		}
	}
	if numbers["Jmin"] > numbers["Jmax"] {
		return errors.New("invalid AmneziaWG junk packet range")
	}
	return checkProtectionPadding(intf, numbers)
}

func checkProtectionPadding(intf map[string]string, numbers map[string]uint64) error {
	if intf["HeaderProtectionKey"] != "" {
		for _, name := range []string{"S1", "S2", "S3", "S4"} {
			if numbers[name] < 12 {
				return errors.New("AmneziaWG header protection requires S1-S4 of at least 12")
			}
		}
	}
	if value := intf["ContentPaddingAddition"]; value != "" {
		if _, ok := parseRange(value, 4096); !ok {
			return invalidOption("ContentPaddingAddition")
		}
	}
	return nil
}

func validateHeaders(intf map[string]string) error {
	var headers []valueRange
	for index, name := range []string{"H1", "H2", "H3", "H4"} {
		n := uint64(index + 1)
		header := valueRange{n, n}
		if value := intf[name]; value != "" {
			var ok bool
			header, ok = parseRange(value, 1<<32-1)
			if !ok || header.high-header.low == 1<<32-1 {
				return invalidOption(name)
			}
		}
		for _, prev := range headers {
			if header.low <= prev.high && prev.low <= header.high {
				return errors.New("AmneziaWG header ranges must not overlap")
			}
		}
		headers = append(headers, header)
	}
	return nil
}

func validateSignatures(intf map[string]string) error {
	for _, name := range []string{"I1", "I2", "I3", "I4", "I5"} {
		if value := intf[name]; value != "" && !validSignature(value) {
			return invalidOption(name)
		}
	}
	return nil
}

// validSignature bounds the emitted packet before the upstream parser can
// allocate memory from input-controlled lengths.
func validSignature(value string) bool {
	const maxPacket = 4096
	total := 0
	for value != "" {
		value = strings.TrimSpace(value)
		if !strings.HasPrefix(value, "<") {
			return false
		}
		tag, rest, ok := strings.Cut(value[1:], ">")
		if !ok {
			return false
		}
		size, ok := signatureTag(strings.Fields(tag))
		if !ok || size > maxPacket-total {
			return false
		}
		total += size
		value = strings.TrimSpace(rest)
	}
	return total > 0
}

func signatureTag(parts []string) (int, bool) {
	switch {
	case len(parts) == 1 && parts[0] == "t":
		return 4, true
	case len(parts) != 2:
		return 0, false
	}
	switch parts[0] {
	case "b":
		return signatureHexSize(parts[1])
	case "r", "rc", "rd":
		n, err := strconv.ParseUint(parts[1], 10, 16)
		return int(n), err == nil && n > 0
	default:
		return 0, false
	}
}

func signatureHexSize(value string) (int, bool) {
	hexText, ok := strings.CutPrefix(value, "0x")
	if !ok || len(hexText) == 0 || len(hexText) > 8192 {
		return 0, false
	}
	data, err := hex.DecodeString(hexText)
	return len(data), err == nil
}

func validateTimings(intf, peer map[string]string) error {
	for _, name := range []string{"RekeyAfterTime", "RekeyTimeout", "RejectAfterTime", "KeepaliveTimeout", "MaxHandshakeAttempts"} {
		if value := intf[name]; value != "" {
			limit := uint64(86400)
			if name == "MaxHandshakeAttempts" {
				limit = 128
			}
			if _, ok := parseRange(value, limit); !ok {
				return invalidOption(name)
			}
		}
	}
	if value := peer["PersistentKeepalive"]; value != "" {
		if _, ok := parseRange(value, 65535); !ok {
			return invalidOption("PersistentKeepalive")
		}
	}
	return nil
}

func normalizeFlags(intf map[string]string) error {
	for _, name := range []string{"RandomTrailers", "DisableCookies"} {
		if value := intf[name]; value != "" {
			flag, err := strconv.ParseBool(value)
			if err != nil {
				return invalidOption(name)
			}
			intf[name] = strconv.FormatBool(flag)
		}
	}
	return nil
}
