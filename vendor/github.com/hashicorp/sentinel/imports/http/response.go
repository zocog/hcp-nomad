package http

import (
	"fmt"
)

// response is an HTTP response returned by a client.
type response struct {
	StatusCode int
	Headers    map[string]string
	Body       string
}

// framework.Map impl.
func (r *response) Map() (map[string]interface{}, error) {
	return map[string]interface{}{
		"status_code": r.StatusCode,
		"headers":     r.Headers,
		"body":        r.Body,
	}, nil
}

// framework.Namespace impl.
func (r *response) Get(key string) (interface{}, error) {
	m, err := r.Map()
	if err != nil {
		return nil, err
	}

	if k, ok := m[key]; ok {
		return k, nil
	} else {
		return nil, fmt.Errorf("unsupported field: %s", key)
	}
}
