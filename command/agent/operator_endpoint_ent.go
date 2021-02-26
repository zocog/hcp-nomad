// +build ent

package agent

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/api"
	"github.com/hashicorp/nomad/nomad/structs"
)

func (s *HTTPServer) LicenseRequest(resp http.ResponseWriter, req *http.Request) (interface{}, error) {
	switch req.Method {
	case "GET":
		return s.operatorGetLicense(resp, req)
	case "PUT":
		return s.operatorPutLicense(resp, req)
	default:
		return nil, CodedError(405, ErrInvalidMethod)
	}
}

func (s *HTTPServer) operatorGetLicense(resp http.ResponseWriter, req *http.Request) (interface{}, error) {
	var args structs.LicenseGetRequest

	if s.parse(resp, req, &args.Region, &args.QueryOptions) {
		return nil, nil
	}

	var reply structs.LicenseGetResponse
	if err := s.agent.RPC("License.GetLicense", &args, &reply); err != nil {
		return nil, err
	}

	return api.LicenseReply{
		License: convertToAPILicense(reply.NomadLicense),
		QueryMeta: api.QueryMeta{
			LastIndex:   reply.QueryMeta.Index,
			LastContact: reply.QueryMeta.LastContact,
			KnownLeader: reply.QueryMeta.KnownLeader,
		},
	}, nil
}

func convertToAPILicense(l *license.License) *api.License {
	// If the license has expired it can be nil
	if l == nil {
		return &api.License{}
	}

	var modules []string
	for _, m := range l.Modules {
		modules = append(modules, m.String())
	}

	return &api.License{
		LicenseID:       l.LicenseID,
		CustomerID:      l.CustomerID,
		InstallationID:  l.InstallationID,
		IssueTime:       l.IssueTime,
		StartTime:       l.StartTime,
		ExpirationTime:  l.ExpirationTime,
		TerminationTime: l.TerminationTime,
		Product:         l.Product,
		Flags:           l.Flags,
		Modules:         modules,
		Features:        l.Features.StringList(),
	}
}

func (s *HTTPServer) operatorPutLicense(resp http.ResponseWriter, req *http.Request) (interface{}, error) {
	var args structs.LicenseUpsertRequest
	s.parseWriteRequest(req, &args.WriteRequest)

	buf := bytes.NewBuffer(nil)
	if _, err := io.Copy(buf, req.Body); err != nil {
		return nil, err
	}

	params := req.URL.Query()
	forceRaw := params.Get("force")

	var force bool
	if forceRaw != "" {
		f, err := strconv.ParseBool(forceRaw)
		if err != nil {
			return nil, fmt.Errorf("error parsing force parameter: %w", err)
		}
		force = f
	}

	args.License = &structs.StoredLicense{
		Signed: buf.String(),
		Force:  force,
	}

	var reply structs.GenericResponse
	if err := s.agent.RPC("License.UpsertLicense", &args, &reply); err != nil {
		return nil, err
	}
	return reply, nil
}
