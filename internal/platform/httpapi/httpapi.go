package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/FootGrid/footgrid/internal/platform/problem"
)

type contextKey string

const traceIDKey contextKey = "trace-id"

func TraceID(ctx context.Context) string {
	if traceID, ok := ctx.Value(traceIDKey).(string); ok {
		return traceID
	}
	return "unknown"
}

func WithMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		traceID := request.Header.Get("X-Request-ID")
		if traceID == "" {
			traceID = time.Now().UTC().Format("20060102T150405.000000000")
		}
		writer.Header().Set("X-Request-ID", traceID)
		ctx := context.WithValue(request.Context(), traceIDKey, traceID)
		started := time.Now()
		next.ServeHTTP(writer, request.WithContext(ctx))
		slog.Info("http request", "method", request.Method, "path", request.URL.Path, "trace_id", traceID, "duration_ms", time.Since(started).Milliseconds())
	})
}

func WriteJSON(writer http.ResponseWriter, status int, body any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}

func WriteProblem(writer http.ResponseWriter, status int, title, detail string, request *http.Request) {
	writer.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(problem.New(status, title, detail, TraceID(request.Context())))
}

func HealthHandler(service string, databasePing func(context.Context) error) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if databasePing != nil {
			if err := databasePing(request.Context()); err != nil {
				WriteProblem(writer, http.StatusServiceUnavailable, "dependency-unavailable", "database unavailable", request)
				return
			}
		}
		WriteJSON(writer, http.StatusOK, map[string]string{"service": service, "status": "ok"})
	}
}

// StartLambda adapts the standard-library handler to API Gateway HTTP API. Keep
// business handlers framework-agnostic so local tests and Lambda share behavior.
func StartLambda(handler http.Handler) {
	lambda.Start(func(ctx context.Context, event events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
		request, err := http.NewRequestWithContext(ctx, event.RequestContext.HTTP.Method, "https://lambda.local"+event.RawPath, strings.NewReader(event.Body))
		if err != nil {
			return events.APIGatewayV2HTTPResponse{StatusCode: http.StatusBadRequest}, nil
		}
		for key, value := range event.Headers {
			request.Header.Set(key, value)
		}
		recorder := newResponseRecorder()
		handler.ServeHTTP(recorder, request)
		headers := make(map[string]string, len(recorder.header))
		for key, values := range recorder.header {
			if len(values) > 0 {
				headers[key] = values[0]
			}
		}
		return events.APIGatewayV2HTTPResponse{StatusCode: recorder.status, Headers: headers, Body: recorder.body.String()}, nil
	})
}

type responseRecorder struct {
	header http.Header
	status int
	body   strings.Builder
}

func newResponseRecorder() *responseRecorder { return &responseRecorder{header: make(http.Header), status: http.StatusOK} }
func (r *responseRecorder) Header() http.Header { return r.header }
func (r *responseRecorder) WriteHeader(status int) { r.status = status }
func (r *responseRecorder) Write(body []byte) (int, error) { return r.body.Write(body) }
