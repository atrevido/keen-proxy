// Command bench measures AEAD throughput as an upper bound for userspace tunnels.
// Real tunnels also pay packet-copy, syscall and firewall costs.
//
//	./bench            # Three seconds per case, all cores
//	./bench -d 5s -p 1 # Five seconds, one worker
package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/chacha20poly1305"
)

// aeadCase names a cipher factory so every worker owns its own mutable AEAD.
type aeadCase struct {
	name string
	make func() cipher.AEAD
}

// main compares AEAD workloads across selected worker counts and packet sizes.
func main() {
	dur := flag.Duration("d", 3*time.Second, "duration of each benchmark")
	par := flag.Int("p", runtime.NumCPU(), "number of concurrent workers")
	flag.Parse()

	fmt.Printf("bench: GOOS=%s GOARCH=%s NumCPU=%d threads=%d duration=%s\n",
		runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), *par, *dur)
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			ll := strings.ToLower(l)
			if strings.HasPrefix(ll, "model name") || strings.HasPrefix(ll, "system type") ||
				strings.HasPrefix(ll, "cpu model") || strings.HasPrefix(ll, "features") {
				fmt.Println("  ", strings.TrimSpace(l))
			}
		}
	}

	key32 := mustRand(32)
	key16 := mustRand(16)
	cases := []aeadCase{
		{"chacha20poly1305 (WireGuard/AWG)", func() cipher.AEAD { a, _ := chacha20poly1305.New(key32); return a }},
		{"aes-128-gcm (TLS/Reality/QUIC)", func() cipher.AEAD { b, _ := aes.NewCipher(key16); a, _ := cipher.NewGCM(b); return a }},
		{"aes-256-gcm", func() cipher.AEAD { b, _ := aes.NewCipher(key32); a, _ := cipher.NewGCM(b); return a }},
	}

	// Compare a typical WireGuard packet with a maximum TLS record.
	sizes := []int{1420, 16384}
	for _, c := range cases {
		for _, size := range sizes {
			mbps := run(c.make, size, *par, *dur)
			fmt.Printf("%-34s %6d B  %8.1f Mbit/s\n", c.name, size, mbps)
		}
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	fmt.Printf("heap_sys=%.1f MiB\n", float64(ms.HeapSys)/(1<<20))
}

// run returns aggregate encryption throughput in Mbit/s for independent workers.
func run(mk func() cipher.AEAD, size, par int, dur time.Duration) float64 {
	var total atomic.Int64
	var wg sync.WaitGroup
	deadline := time.Now().Add(dur)
	start := time.Now()
	for i := 0; i < par; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a := mk()
			nonce := make([]byte, a.NonceSize())
			src := make([]byte, size)
			dst := make([]byte, 0, size+a.Overhead())
			var n int64
			for k := 0; ; k++ {
				dst = a.Seal(dst[:0], nonce, src, nil)
				n += int64(size)
				if k&63 == 0 && time.Now().After(deadline) {
					break
				}
			}
			total.Add(n)
		}()
	}
	wg.Wait()
	return float64(total.Load()) * 8 / time.Since(start).Seconds() / 1e6
}

// mustRand creates benchmark material and aborts if secure entropy is unavailable.
func mustRand(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}
