package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"

	"devflow/internal/flowdemo"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18080", "HTTP listen address")
	root := flag.String("root", filepath.Join("runtime", "flowdemo"), "flow demo run root")
	flag.Parse()

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		listener, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			log.Fatal(err)
		}
	}
	service := flowdemo.NewService(*root)
	fmt.Printf("flowdemo listening on http://%s\n", listener.Addr().String())
	if err := http.Serve(listener, flowdemo.NewHandler(service)); err != nil {
		log.Fatal(err)
	}
}
