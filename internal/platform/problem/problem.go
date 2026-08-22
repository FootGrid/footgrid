package problem

import "net/http"

// Problem follows RFC 9457 application/problem+json responses.
type Problem struct {
	Type    string `json:"type,omitempty"`
	Title   string `json:"title"`
	Status  int    `json:"status"`
	Detail  string `json:"detail,omitempty"`
	TraceID string `json:"trace_id"`
}

// TODO(anshu): Need to add more types
func New(status int, title, detail, traceID string) Problem {
	return Problem{Type: "https://api.footgrid.example/problems/" + title, Title: title, Status: status, Detail: detail, TraceID: traceID}
}

func Status(err error) int {
	if err == nil {
		return http.StatusOK
	}
	return http.StatusInternalServerError
}
