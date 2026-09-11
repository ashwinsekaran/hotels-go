// Package health provides the Kubernetes liveness and readiness endpoints:
//
//	GET /.well-known/live   liveness  — is the process alive? Always 200 while running.
//	GET /.well-known/ready  readiness — should traffic be routed here? 200 or 503.
//
// The split matters to Kubernetes: a failing liveness probe makes the kubelet
// RESTART the pod, whereas a failing readiness probe only removes the pod from
// Service endpoints (no restart). On graceful shutdown, flip readiness to
// not-ready first so the load balancer stops sending new requests while in-flight
// ones drain — the pod stays live throughout. Mount these OUTSIDE the auth
// middleware in main; the kubelet probes them without credentials.
package health

import (
	"net/http"
	"sync/atomic"
)

// Live is the liveness handler: it returns 200 as long as the process can serve
// HTTP at all. It carries no dependencies on purpose — liveness must not fail
// just because a downstream (DB, cache) is momentarily unavailable, or k8s would
// restart an otherwise-healthy pod.
func Live(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// Readiness reports whether the service should currently receive traffic. The
// zero value is NOT ready; call SetReady(true) once wiring is complete. The flag
// is an atomic.Bool so the shutdown goroutine and probe requests touch it
// concurrently without a mutex.
type Readiness struct {
	ready atomic.Bool
}

// SetReady toggles the readiness state: true after wiring is done, false at the
// start of graceful shutdown.
func (rd *Readiness) SetReady(ready bool) {
	rd.ready.Store(ready)
}

// Handler is the readiness endpoint: 200 when ready, 503 Service Unavailable
// otherwise, so Kubernetes pulls the pod out of rotation until it recovers or
// during shutdown.
func (rd *Readiness) Handler(w http.ResponseWriter, r *http.Request) {
	if !rd.ready.Load() {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}
