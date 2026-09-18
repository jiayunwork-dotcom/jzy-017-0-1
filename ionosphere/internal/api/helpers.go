package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}
