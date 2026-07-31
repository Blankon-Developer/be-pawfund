package main

import (
	"fmt"
	"net/http"
	"time"
)

func main() {
	fmt.Println("Hello world")

	http.HandleFunc("/health", HealthCheck)
	server := &http.Server{
		Addr:         ":8080",
		IdleTimeout:  time.Minute,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	fmt.Println("App started")

	err := server.ListenAndServe()
	if err != nil {
		panic(err)
	}
}

func HealthCheck(w http.ResponseWriter, r *http.Request) {
	_, _ = fmt.Fprintf(w, "Hello from backend")
}
