package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func hello(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(w, "Hello :) \n")
}

func main() {
	http.HandleFunc("/", hello)
	http.Handle("/metrics", promhttp.Handler())

	port := ":2112"
	log.Printf("Starting server on port %s \n", port)
	log.Fatal(http.ListenAndServe(port, nil))
}
