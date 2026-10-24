package main

import (
	"context"
	"fmt"
	"net"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/dht/v2/int160"
)

func main() {
	server, err := dht.NewServer(nil)
	if err != nil {
		panic(err)
	}
	defer server.Close()

	addr := dht.NewAddr(&net.UDPAddr{
		IP:   net.ParseIP("1.2.3.4"),
		Port: 6881,
	})

	var infoHash int160.T
	// infoHash = ...

	result := server.GetPeers(
		context.Background(),
		addr,
		infoHash,
		false, // scrape
		dht.QueryRateLimiting{},
	)

	fmt.Printf("%+v\n", result)
}
