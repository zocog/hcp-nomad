// +build ent

package agent

import (
	"net/http"

	"github.com/hashicorp/nomad/api"
	"github.com/hashicorp/nomad/nomad/structs"
)

func (s *HTTPServer) OperatorLicenseRequest(resp http.ResponseWriter, req *http.Request) (interface{}, error) {
	switch req.Method {
	case "GET":
		return s.operatorGetLicense(resp, req)
	case "PUT":
		return s.operatorPutLicense(resp, req)
	case "DELETE":
		return s.operatorResetLicense(resp, req)
	default:
		return nil, CodedError(404, ErrInvalidMethod)
	}
}

func (s *HTTPServer) operatorGetLicense(resp http.ResponseWriter, req *http.Request) (interface{}, error) {
	var args structs.LicenseGetRequest

	if s.parse(resp, req, &args.Region, &args.QueryOptions) {
		return nil, nil
	}

	// TODO change to structs.LicenseResponse
	var reply structs.LicenseGetResponse
	if err := s.agent.RPC("Operator.GetLicense", &args, &reply); err != nil {
		return nil, err
	}

	return nil, nil
}

func (s *HTTPServer) operatorPutLicense(resp http.ResponseWriter, req *http.Request) (interface{}, error) {
	var args structs.LicensePutRequest

	s.parseWriteRequest(req, &args.WriteRequest)

	// TODO change to structs.LicenseResponse
	var reply api.GenericResponse
	if err := s.agent.RPC("Operator.UpsertLicense", &args, &reply); err != nil {
		return nil, err
	}
	return nil, nil
}

func (s *HTTPServer) operatorResetLicense(resp http.ResponseWriter, req *http.Request) (interface{}, error) {
	var args structs.LicenseResetRequest

	s.parseWriteRequest(req, &args.WriteRequest)

	// TODO change to structs.LicenseResponse
	var reply api.GenericResponse
	if err := s.agent.RPC("Operator.ResetLicense", &args, &reply); err != nil {
		return nil, err
	}
	return nil, nil
}
