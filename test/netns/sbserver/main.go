// Command sbserver runs a sing-box server from the integration fixture configuration.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	sjson "github.com/sagernet/sing/common/json"
)

// main serves the synthetic WAN endpoints until the test terminates the process.
func main() {
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	ctx := include.Context(context.Background())
	opts, err := sjson.UnmarshalExtendedContext[option.Options](ctx, raw)
	if err != nil {
		log.Fatal(err)
	}
	instance, err := box.New(box.Options{Context: ctx, Options: opts})
	if err != nil {
		log.Fatal(err)
	}
	if err := instance.Start(); err != nil {
		log.Fatal(err)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	instance.Close()
}
