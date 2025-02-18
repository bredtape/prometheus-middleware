package prometheusmiddleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	defaultBuckets = []float64{0.3, 1.0, 2.5, 5.0}
	labelKeys      = []string{"code", "method", "path"}
)

const (
	requestName = "http_requests_total"
	latencyName = "http_request_duration_seconds"
)

// Opts specifies options how to create new PrometheusMiddleware.
type Opts struct {
	// Buckets specifies an custom buckets to be used in request histograpm.
	Buckets []float64

	// Prometheus register. Specify to not use the default
	Registerer prometheus.Registerer
}

func (o Opts) WithDefaults() Opts {
	if len(o.Buckets) == 0 {
		o.Buckets = defaultBuckets
	}
	if o.Registerer == nil {
		o.Registerer = prometheus.DefaultRegisterer
	}
	return o
}

// PrometheusMiddleware specifies the metrics that is going to be generated
type PrometheusMiddleware struct {
	request *prometheus.CounterVec
	latency *prometheus.HistogramVec
}

// NewPrometheusMiddleware creates a new PrometheusMiddleware instance
func NewPrometheusMiddleware(opts Opts) (*PrometheusMiddleware, error) {

	opts = opts.WithDefaults()

	request := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: requestName,
			Help: "How many HTTP requests processed, partitioned by status code, method and HTTP path.",
		}, labelKeys)

	if err := opts.Registerer.Register(request); err != nil {
		return nil, errors.Wrap(err, "failed to register metric "+requestName)
	}

	latency := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    latencyName,
		Help:    "How long it took to process the request, partitioned by status code, method and HTTP path.",
		Buckets: opts.Buckets},
		labelKeys)

	if err := opts.Registerer.Register(latency); err != nil {
		return nil, errors.Wrap(err, "failed to register metric "+latencyName)
	}

	return &PrometheusMiddleware{
		request: request,
		latency: latency}, nil
}

// InstrumentHandlerDuration is a middleware that wraps the http.Handler and it record
// how long the handler took to run, which path was called, and the status code.
// This method is going to be used with gorilla/mux.
func (p *PrometheusMiddleware) InstrumentHandlerDuration(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		begin := time.Now()

		delegate := &responseWriterDelegator{ResponseWriter: w}
		rw := delegate

		next.ServeHTTP(rw, r) // call original

		route := mux.CurrentRoute(r)
		path, _ := route.GetPathTemplate()

		code := sanitizeCode(delegate.status)
		method := sanitizeMethod(r.Method)

		p.request.WithLabelValues(
			code,
			method,
			path,
		).Inc()

		p.latency.WithLabelValues(
			code,
			method,
			path,
		).Observe(time.Since(begin).Seconds())
	})
}

type responseWriterDelegator struct {
	http.ResponseWriter
	status      int
	written     int64
	wroteHeader bool
}

func (r *responseWriterDelegator) WriteHeader(code int) {
	r.status = code
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseWriterDelegator) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(b)
	r.written += int64(n)
	return n, err
}

func sanitizeMethod(m string) string {
	return strings.ToLower(m)
}

func sanitizeCode(s int) string {
	return strconv.Itoa(s)
}
