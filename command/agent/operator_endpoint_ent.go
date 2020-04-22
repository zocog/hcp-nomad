// +build ent

package agent

import (
	"net/http"

	"github.com/hashicorp/consul-enterprise/api"
	license "github.com/hashicorp/go-licensing"
)

func (s *HTTPServer) OperatorLicenseRequest(resp http.ResponseWriter, req *http.Request) (interface{}, error) {
	switch req.Method {
	case "GET":
		return s.OperatorGetLicense(resp, req)
	case "PUT":
		return s.OperatorPutLicense(resp, req)
	case "DELETE":
		return s.OperatorResetLicense(resp, req)
	default:
		return nil, CodedError(404, ErrInvalidMethod)
	}
}

func (s *HTTPServer) OperatorGetLicense(resp http.ResponseWriter, req *http.Request) (interface{}, error) {
	// var args structs.LicenseGetRequest

	return nil, nil
}

func (s *HTTPServer) OperatorPutLicense(resp http.ResponseWriter, req *http.Request) (interface{}, error) {

	return nil, nil
}

func (s *HTTPServer) OperatorResetLicense(resp http.ResponseWriter, req *http.Request) (interface{}, error) {

	return nil, nil
}

func convertLicense(l *license.License) *api.License {
	modules := make([]string, len(l.Modules))
	for i, mod := range l.Modules {
		modules[i] = mod.DisplayString()
	}

	return &api.License{}
}
