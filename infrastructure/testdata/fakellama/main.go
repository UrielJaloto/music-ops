package main

import (
	"net/http"
	"os"
	"strings"
	"time"
)

const readyDelay = 500 * time.Millisecond

func main() {
	port := argumentAfter("--port")
	if strings.HasSuffix(argumentAfter("-m"), "fail.gguf") {
		_, _ = os.Stderr.WriteString("failed to load model: out of memory")
		os.Exit(1)
	}
	started := time.Now()
	http.HandleFunc("/health", func(writer http.ResponseWriter, _ *http.Request) {
		if time.Since(started) < readyDelay {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusOK)
	})
	server := &http.Server{Addr: "127.0.0.1:" + port, ReadHeaderTimeout: time.Second}
	_ = server.ListenAndServe()
}

func argumentAfter(flag string) string {
	for index, argument := range os.Args {
		if argument == flag && index+1 < len(os.Args) {
			return os.Args[index+1]
		}
	}
	return ""
}
