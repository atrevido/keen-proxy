package engine

import (
	"bufio"
	"os"
	"strings"
	"testing"
	"time"
)

// sing-box writes to a /dev/fd/N pipe so the daemon can collect system logs
// without creating log files on storage.
func TestLogOutputPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	lines := make(chan string, 10)
	go func() {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	e := Engine{LogOutput: PipeLogPath(w.Fd())}
	if _, err := e.Apply([]byte(`{"log":{"level":"info","timestamp":true},"outbounds":[{"type":"direct","tag":"direct"}]}`)); err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	select {
	case l := <-lines:
		if !strings.Contains(l, "INFO") {
			t.Errorf("log line: %q", l)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("sing-box wrote nothing to the channel")
	}
}
