package domaincache

import (
	"fmt"
	"github.com/miekg/dns"
	"sync"
	"testing"
	"time"
)

func answer(lines ...string) *dns.Msg {
	m := new(dns.Msg)
	for _, s := range lines {
		r, err := dns.NewRR(s)
		if err != nil {
			panic(err)
		}
		m.Answer = append(m.Answer, r)
	}
	return m
}

func TestObserveCNAMEExpiry(t *testing.T) {
	now := time.Unix(1000, 0)
	c := New(func() time.Time { return now })
	c.Observe("www.example.com", answer("www.example.com. 10 IN CNAME cdn.example.net.", "cdn.example.net. 60 IN A 203.0.113.1", "unrelated.test. 90 IN A 203.0.113.2"), "observed")
	r := c.Snapshot(nil)
	if len(r) != 1 || len(r[0].Addresses) != 1 || r[0].Addresses[0].IP != "203.0.113.1" || !r[0].Addresses[0].ExpiresAt.Equal(now.Add(10*time.Second)) {
		t.Fatalf("%+v", r)
	}
	c.SetError("www.example.com", "timeout")
	now = now.Add(11 * time.Second)
	if r = c.Snapshot(nil); len(r[0].Addresses) != 0 || r[0].Error != "timeout" {
		t.Fatalf("%+v", r)
	}
}

func TestZeroTTLAndRefresh(t *testing.T) {
	c := New(time.Now)
	c.Observe("example.com", answer("example.com. 60 IN A 203.0.113.1"), "manual")
	c.Observe("example.com", answer("example.com. 0 IN A 203.0.113.2"), "manual")
	if len(c.Snapshot(nil)[0].Addresses) != 0 {
		t.Fatal("zero TTL retained")
	}
}

func TestLimitsAndEviction(t *testing.T) {
	c := New(time.Now)
	m := answer()
	for i := 1; i < 40; i++ {
		r, _ := dns.NewRR(fmt.Sprintf("example.com. 60 IN A 203.0.113.%d", i))
		m.Answer = append(m.Answer, r)
	}
	c.Observe("example.com", m, "manual")
	if len(c.Snapshot(nil)[0].Addresses) != 32 || !c.Snapshot(nil)[0].Limited {
		t.Fatal("address limit")
	}
	for i := 0; i < 4100; i++ {
		c.SetError(fmt.Sprintf("%d.example.com", i), "timeout")
	}
	if len(c.Snapshot(nil)) != 4096 || !c.Limited() {
		t.Fatal("name limit")
	}
}

func TestObserveConcurrent(t *testing.T) {
	c := New(time.Now)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				c.Observe("example.com", answer("example.com. 60 IN A 203.0.113.1"), "observed")
				c.Snapshot(nil)
				c.Prune()
			}
		}()
	}
	wg.Wait()
}

func TestCNAMECycleIsNotAnAddress(t *testing.T) {
	c := New(time.Now)
	c.Observe("a.test", answer("a.test. 60 IN CNAME b.test.", "b.test. 60 IN CNAME a.test.", "a.test. 60 IN A 203.0.113.1"), "observed")
	if len(c.Snapshot(nil)[0].Addresses) != 0 {
		t.Fatal("cyclic answer learned an IP")
	}
}

func TestServiceRefreshDoesNotCountAsObservation(t *testing.T) {
	now := time.Unix(1000, 0)
	c := New(func() time.Time { return now })
	m := answer("example.com. 60 IN A 203.0.113.1")
	c.Observe("example.com", m, "observed")
	observed := now
	now = now.Add(time.Minute)
	c.Observe("example.com", m, "resolved")
	r := c.Snapshot(nil)[0]
	if !r.ObservedAt.Equal(observed) || !r.CheckedAt.Equal(now) {
		t.Fatal(r)
	}
}
