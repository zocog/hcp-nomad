package http

import (
	"encoding/base64"
	"fmt"
	"regexp"
)

var restrictedRequestHeaders []*regexp.Regexp = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^Host$`),
	regexp.MustCompile(`(?i)^User-Agent$`),
	regexp.MustCompile(`(?i)^Referer$`),
	regexp.MustCompile(`(?i)^Connection$`),
	regexp.MustCompile(`(?i)^Content-Length$`),
	regexp.MustCompile(`(?i)^Keep-Alive$`),
	regexp.MustCompile(`(?i)^Origin$`),
	regexp.MustCompile(`(?i)^X-Forwarded-.*$`),
}

// request encapsulates an HTTP request for a client to execute.
type request struct {
	URL     string            `mapstructure:"url"`
	Headers map[string]string `mapstructure:"headers"`
}

// newRequest returns a newly initialized httpRequest type with the correct defaults
func newRequest(url string) *request {
	var defaultHeaders = map[string]string{
		"Accept": "*/*;charset=UTF-8",
	}

	return &request{
		URL:     url,
		Headers: defaultHeaders,
	}
}

// setHeader sets a key/value in the Headers hash
func (r *request) setHeader(key, value string) error {
	for _, exp := range restrictedRequestHeaders {
		if exp.MatchString(key) {
			return fmt.Errorf("cannot set restricted header %q", key)
		}
	}

	r.Headers[key] = value
	return nil
}

// setBasicAuth configures HTTP basic authentication for the request.
func (r *request) setBasicAuth(username, password string) {
	auth := username + ":" + password
	encoded := base64.StdEncoding.EncodeToString([]byte(auth))
	r.Headers["Authorization"] = "Basic " + encoded
}

// framework.Map impl.
func (r *request) Map() (map[string]interface{}, error) {
	return map[string]interface{}{
		"url":     r.URL,
		"headers": r.Headers,

		// Used to identify the object as a request for framework.New
		"_type": "request",
	}, nil
}

// framework.Namespace impl.
func (r *request) Get(key string) (interface{}, error) {
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

// framework.Call impl.
func (r *request) Func(key string) interface{} {
	switch key {
	case "with_header":
		return r.withHeader
	case "with_basic_auth":
		return r.withBasicAuth
	}

	return nil
}

func (r *request) clone() *request {
	headers := make(map[string]string, len(r.Headers))
	for k, v := range r.Headers {
		headers[k] = v
	}
	return &request{
		URL:     r.URL,
		Headers: headers,
	}
}

func (r *request) withHeader(key, value string) (interface{}, error) {
	clone := r.clone()
	err := clone.setHeader(key, value)
	if err != nil {
		return nil, err
	}
	return clone, nil
}

func (r *request) withBasicAuth(username, password string) interface{} {
	clone := r.clone()
	clone.setBasicAuth(username, password)
	return clone
}
