// Command healthcheck is a minimal liveness probe for the distroless runtime
// image. distroless ships no shell, curl, or wget, so a Docker HEALTHCHECK has
// nothing to invoke; this static binary fills that gap. It performs GET
// /healthz against the local API server and exits 0 on HTTP 200, non-zero
// otherwise.
//
// The target port follows the same PORT environment variable the API server
// reads (config default 7272), so the probe and the server can never disagree
// on which port to use. The probe hits liveness (/healthz), not readiness
// (/readyz): a transient database outage must not mark the container unhealthy
// and trigger a restart.
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "7272"
	}

	url := "http://" + net.JoinHostPort("127.0.0.1", port) + "/healthz"
	client := &http.Client{Timeout: 3 * time.Second}

	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: GET %s: %v\n", url, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: GET %s: status %d\n", url, resp.StatusCode)
		os.Exit(1)
	}
}
