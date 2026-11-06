package journal

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"time"
)

// Handler mirrors slog records into the system journal, excluding debug messages.
type Handler struct {
	slog.Handler
	J      *Journal
	attrs  []slog.Attr
	groups string
}

// NewHandler wraps an existing slog handler with system-journal mirroring.
func NewHandler(next slog.Handler, j *Journal) *Handler {
	return &Handler{Handler: next, J: j}
}

// Handle mirrors non-debug records before forwarding them to the wrapped handler.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelInfo {
		var b strings.Builder
		b.WriteString(r.Message)
		add := func(a slog.Attr) bool {
			fmt.Fprintf(&b, " %s%s=%v", h.groups, a.Key, a.Value)
			return true
		}
		for _, a := range h.attrs {
			add(a)
		}
		r.Attrs(add)
		h.J.Add(Entry{Time: r.Time, Level: levelName(r.Level), Source: "keenproxy", Message: b.String()})
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs preserves journal mirroring while adding attributes to the wrapped handler.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{Handler: h.Handler.WithAttrs(attrs), J: h.J, attrs: append(append([]slog.Attr{}, h.attrs...), attrs...), groups: h.groups}
}

// WithGroup preserves journal mirroring while grouping the wrapped handler's attributes.
func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{Handler: h.Handler.WithGroup(name), J: h.J, attrs: h.attrs, groups: h.groups + name + "."}
}

func levelName(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "ERROR"
	case l >= slog.LevelWarn:
		return "WARN"
	}
	return "INFO"
}

// Uncolored sing-box line: "+0300 2026-11-05 23:24:50 WARN [1234 5ms] outbound/fi: …".
var reSingBox = regexp.MustCompile(`^(?:[+-]\d{4} \d{4}-\d\d-\d\d \d\d:\d\d:\d\d )?(TRACE|DEBUG|INFO|WARN|ERROR|FATAL|PANIC)\s+(?:\[[^\]]*\]\s+)?(.*)$`)

// ReadSingBox reads sing-box log lines until EOF and appends them to j.
func ReadSingBox(r io.Reader, j *Journal, mirror io.Writer) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 16<<10), 256<<10)
	for sc.Scan() {
		line := sc.Text()
		if mirror != nil {
			fmt.Fprintln(mirror, line)
		}
		level, msg := "INFO", line
		if m := reSingBox.FindStringSubmatch(line); m != nil {
			level, msg = m[1], m[2]
		}
		switch level {
		case "TRACE", "DEBUG":
			continue
		case "FATAL", "PANIC":
			level = "ERROR"
		}
		j.Add(Entry{Time: time.Now(), Level: level, Source: "sing-box", Message: msg})
	}
}
