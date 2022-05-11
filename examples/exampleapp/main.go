package main

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var httpRequestsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of GET requests",
	},
	[]string{"path"},
)

func hello(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(w, "Hello :) \n")
	httpRequestsTotal.WithLabelValues("root").Inc()
}

func main() {
	http.HandleFunc("/", hello)
	http.Handle("/metrics", promhttp.Handler())

	prometheus.MustRegister(httpRequestsTotal)

	port := ":2112"
	log.Printf("Starting server on port %s \n", port)
	log.Fatal(http.ListenAndServe(port, nil))
}
