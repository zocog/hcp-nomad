package http

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	netHTTP "net/http"
	netURL "net/url"
	"regexp"
	"strings"
	"time"

	cleanhttp "github.com/hashicorp/go-cleanhttp"
	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"github.com/mitchellh/mapstructure"
)

var (
	netHTTPRedirectRe = regexp.MustCompile(`stopped after \d+ redirects`)
	errProxyConnect   = errors.New("internal proxy connection error")
)

// client is a general HTTP client with Sentinel-specific functionality and
// helpers.
type client struct {
	// Status codes which will not trigger a runtime error when received.
	AcceptedStatusCodes []int `mapstructure:"accepted_status_codes"`

	// The number of times the client should retry before returning an error.
	Retries int `mapstructure:"retries"`

	// Request timeout, in seconds.
	Timeout int `mapstructure:"timeout"`

	// When false, allows the use of TLS certificates from untrusted CA's.
	CertificateVerification bool `mapstructure:"certificate_verification"`

	// Import-level configuration.
	config *config

	// The HTTP client to use for making requests. Use the http() function
	// to obtain a client, and it will be automatically saved here for further
	// invocations.
	httpClient *retryablehttp.Client
}

// newClient constructs a new client with sane defaults.
func newClient(config *config) *client {
	return &client{
		AcceptedStatusCodes:     []int{200},
		Retries:                 1,
		Timeout:                 10,
		CertificateVerification: true,
		config:                  config,
	}
}

// getURL executes an HTTP request with a URL string.
func (c *client) getURL(url string) (*response, error) {
	return c.getRequest(newRequest(url))
}

// getRequest executes an HTTP request with a request type
func (c *client) getRequest(request *request) (*response, error) {
	parsedURL, err := c.parseURL(request.URL)
	if err != nil {
		return nil, err
	}

	req, err := retryablehttp.NewRequest("GET", parsedURL, nil)
	if err != nil {
		return nil, err
	}

	for k, v := range request.Headers {
		req.Header.Set(k, v)
	}

	resp, err := c.http().Do(req)
	if err != nil {
		return nil, massageHTTPError(err)
	}
	defer resp.Body.Close()

	if err := c.checkResponse(resp); err != nil {
		return nil, err
	}

	headers := make(map[string]string, len(resp.Header))
	for k, v := range resp.Header {
		headers[k] = strings.Join(v, ",")
	}

	// Limit the actual response reader to ensure we never read more than the
	// maximum body size. This will allow us to produce an error for clients
	// which send erroneous requests with a false Content-Length header which
	// has a lower value than the actual number of bytes in the payload.
	var br io.Reader = resp.Body
	if c.config.MaxResponseBytes > 0 {
		br = io.LimitReader(resp.Body, c.config.MaxResponseBytes+1) // see below
	}

	body, err := ioutil.ReadAll(br)
	if err != nil {
		return nil, err
	}

	// Check for an exhausted reader. The io.LimitedReader implementation
	// returns io.EOF when the maximum size has been reached. This results
	// in the body getting a short read, but producing no errors. We want
	// to error out when a payload exceeds the limit instead. To accomplish
	// this, we allow one additional byte to be read in the LimitedReader,
	// and we check the remaining bytes here. If there are no bytes available,
	// then we know we've read more than the max size.
	if r, ok := br.(*io.LimitedReader); ok {
		if r.N <= 0 {
			return nil, fmt.Errorf("response body too large (max %d bytes)",
				c.config.MaxResponseBytes)
		}
	}

	return &response{
		StatusCode: resp.StatusCode,
		Headers:    headers,
		Body:       string(body),
	}, nil
}

// checkResponse ensures that the given response matches the configured
// acceptable response criteria, and returns an error if it doesn't.
func (c *client) checkResponse(resp *netHTTP.Response) error {
	// Check status codes.
	statusOk := false
	for _, code := range c.AcceptedStatusCodes {
		if resp.StatusCode == code {
			statusOk = true
			break
		}
	}

	if !statusOk {
		return fmt.Errorf("expected status code to be one of %v, got %d",
			c.AcceptedStatusCodes, resp.StatusCode)
	}

	// Check the Content-Length header. This is an easy fast-path for returning
	// an error if this value is larger than the maximum allowed size. We will
	// still check during actual body reading as well, but this allows us to
	// avoid reading up to the maximum size in normal cases.
	if max := c.config.MaxResponseBytes; max > 0 && resp.ContentLength > max {
		return fmt.Errorf("response body too large (max %d bytes, got %d bytes)",
			max, resp.ContentLength)
	}

	return nil
}

// parseURL parses the given URL and ensures it is valid. Custom validations
// also belong here for things like checking if a user/pass combo is present
// in the URL for basic auth.
func (c *client) parseURL(url string) (string, error) {
	parsed, err := netURL.Parse(url)
	if err != nil {
		return "", err
	}

	if _, pw := parsed.User.Password(); pw {
		return "", fmt.Errorf("URL encoding of Basic Authentication credentials is not allowed")
	}

	return url, nil
}

// http returns a retrying HTTP client to use based on the settings configured
// in the client.
func (c *client) http() *retryablehttp.Client {
	if c.httpClient == nil {
		// Configure the transport's TLS settings.
		transport := cleanhttp.DefaultTransport()
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: !c.CertificateVerification,
		}

		// Add in the proxy, if any.
		if c.config.proxyURL != nil {
			transport.Proxy = netHTTP.ProxyURL(c.config.proxyURL)
		}

		// Create the retryablehttp client and apply configuration to it.
		rc := retryablehttp.NewClient()
		rc.ErrorHandler = retryablehttp.PassthroughErrorHandler
		rc.RetryMax = c.Retries
		rc.HTTPClient.Timeout = time.Duration(c.Timeout) * time.Second
		rc.HTTPClient.Transport = transport
		// Disable logging. This particularly makes the CLI less noisy
		// and also helps guard against exfiltration issues when tokens
		// end up in the URL/query string.
		rc.Logger = nil

		// Cache the retryablehttp client for further uses.
		c.httpClient = rc
	}

	return c.httpClient
}

// -------------------- SDK framework functions --------------------

// framework.Map impl.
func (c *client) Map() (map[string]interface{}, error) {
	return map[string]interface{}{
		"accepted_status_codes":    c.AcceptedStatusCodes,
		"retries":                  c.Retries,
		"timeout":                  c.Timeout,
		"certificate_verification": c.CertificateVerification,

		// Used to identify the object as a client for framework.New
		"_type": "client",
	}, nil
}

// framework.Namespace impl.
func (c *client) Get(key string) (interface{}, error) {
	m, err := c.Map()
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
func (c *client) Func(key string) interface{} {
	switch key {
	case "get":
		return c.httpGet
	case "accept_status_codes":
		return c.acceptStatusCodes
	case "with_retries":
		return c.withRetries
	case "with_timeout":
		return c.withTimeout
	case "without_certificate_verification":
		return c.withoutCertificateVerification
	}

	return nil
}

// httpGet executes a GET request with the provided URL of request structure.
func (c *client) httpGet(urlOrRequest interface{}) (interface{}, error) {
	switch v := urlOrRequest.(type) {
	case string:
		return c.getURL(v)
	case map[string]interface{}:
		req := newRequest("")
		if err := mapstructure.WeakDecode(v, req); err != nil {
			return nil, err
		}
		return c.getRequest(req)
	default:
		return nil, fmt.Errorf("invalid argument: %v", v)
	}
}

// acceptStatusCodes allows the caller to set status code expectations.
// Returns a new namespaceClient with the modified status codes.
func (c *client) acceptStatusCodes(statusCodes []int) interface{} {
	clone := c.clone()
	clone.AcceptedStatusCodes = statusCodes

	return clone
}

// withRetries configures the number of retries the HTTP client will
// perform before returning an error.
func (c *client) withRetries(n int) (interface{}, error) {
	if n > maxRetries {
		return nil, fmt.Errorf("invalid number of retries: %d (max %d)",
			n, maxRetries)
	}

	clone := c.clone()
	clone.Retries = n

	return clone, nil
}

// withTimeout configures the request timeout on the client.
func (c *client) withTimeout(t int) (interface{}, error) {
	if t > maxTimeout {
		return nil, fmt.Errorf("invalid timeout: %d (max %d)",
			t, maxTimeout)
	}

	clone := c.clone()
	clone.Timeout = t

	return clone, nil
}

// withoutCertificateVerification disables TLS certificate verification.
func (c *client) withoutCertificateVerification() interface{} {
	clone := c.clone()
	clone.CertificateVerification = false

	return clone
}

// clone clones the namespace and returns the copy.
func (c *client) clone() *client {
	// Create a new client and deep copy all of the values.
	clone := newClient(c.config)
	copy(clone.AcceptedStatusCodes, c.AcceptedStatusCodes)
	clone.Retries = c.Retries
	clone.Timeout = c.Timeout
	clone.CertificateVerification = c.CertificateVerification

	return clone
}

// massageError takes a raw net/http error and presents in a way more
// appropriate for exposure in Sentinel itself.
func massageHTTPError(err error) error {
	if v, ok := err.(*netURL.Error); ok {
		// Check for a proxy error first. We don't send extra detail on
		// these errors as these are internal errors that need to be
		// fixed by the integration operator.
		if opErr, ok := v.Err.(*net.OpError); ok && opErr.Op == "proxyconnect" {
			return errProxyConnect
		}

		// Otherwise handle the regular url.Error errors
		switch {
		case strings.Contains(v.Err.Error(), "Timeout exceeded"):
			return fmt.Errorf("request timed out (%s)", v.URL)
		case netHTTPRedirectRe.MatchString(v.Err.Error()):
			return fmt.Errorf("exceeded maximum number of redirects (%s)", v.URL)
		case strings.Contains(v.Err.Error(), "connection refused"):
			return fmt.Errorf("connection refused (%s)", v.URL)
		case strings.Contains(v.Err.Error(), "gave HTTP response to HTTPS client"):
			return fmt.Errorf("HTTP response received, but expected HTTPS (%s)", v.URL)
		case strings.Contains(v.Err.Error(), "unsupported protocol scheme"):
			return fmt.Errorf("URL contains an unsupported protocol scheme; supported schemes are http or https (%s)", v.URL)
		}
		// Cut out extra "<Op> <URL>: <Err>" in net/http
		return fmt.Errorf("%s (%s)", v.Err.Error(), v.URL)
	}

	return err
}
