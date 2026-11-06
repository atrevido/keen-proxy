package filterlist

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkParse100K(b *testing.B) {
	var text strings.Builder
	for i := 0; i < 100000; i++ {
		fmt.Fprintf(&text, "%06d.tracker.test\n", i)
	}
	data := text.String()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p, err := Parse(strings.NewReader(data))
		if err != nil || len(p.Domains) != 100000 {
			b.Fatal(err, len(p.Domains))
		}
	}
}
