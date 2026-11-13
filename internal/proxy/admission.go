package proxy

import (
	"context"
	"cui-proxy/internal/admission"
	"cui-proxy/internal/observe"
	"net/http"
	"time"
)

const admissionQueueTimeout = 5 * time.Second

// admit runs before metadata inspection or body allocation. Unknown-length
// bodies reserve their entire policy allowance. The caller must use readLimit
// as well: even an inconsistent Content-Length cannot exceed the reservation.
func admit(r *http.Request, limit int64, trace *observe.Trace) (release func(), readLimit int64, err error) {
	defer trace.Measure(observe.AdmissionWait)()
	readLimit = limit
	if r.ContentLength >= 0 && r.ContentLength < readLimit {
		readLimit = r.ContentLength
	}
	ctx, cancel := context.WithTimeout(r.Context(), admissionQueueTimeout)
	defer cancel()
	release, err = admission.Requests.Acquire(ctx, readLimit)
	return
}
