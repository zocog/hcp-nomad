//go:build ent
// +build ent

package agent

import (
	"net/http"
	"strings"

	"github.com/hashicorp/nomad/api"
	"github.com/hashicorp/nomad/nomad/structs"
)

func (s *HTTPServer) RecommendationsListRequest(resp http.ResponseWriter, req *http.Request) (interface{}, error) {
	if req.Method != "GET" {
		return nil, CodedError(405, ErrInvalidMethod)
	}

	jobID := req.URL.Query().Get("job")
	group := req.URL.Query().Get("group")
	task := req.URL.Query().Get("task")
	if group != "" && jobID == "" {
		return nil, CodedError(400, "group query param requires job query param")
	}
	if task != "" && group == "" {
		return nil, CodedError(400, "task query param requires group query param")
	}

	args := structs.RecommendationListRequest{
		JobID: jobID,
		Group: group,
		Task:  task,
	}
	if s.parse(resp, req, &args.Region, &args.QueryOptions) {
		return nil, nil
	}

	var out structs.RecommendationListResponse
	if err := s.agent.RPC("Recommendation.ListRecommendations", &args, &out); err != nil {
		return nil, err
	}

	setMeta(resp, &out.QueryMeta)
	return out.Recommendations, nil
}

func (s *HTTPServer) RecommendationCreateRequest(resp http.ResponseWriter, req *http.Request) (interface{}, error) {
	if req.Method != "PUT" && req.Method != "POST" {
		return nil, CodedError(405, ErrInvalidMethod)
	}

	var aRec api.Recommendation
	if err := decodeBody(req, &aRec); err != nil {
		return nil, CodedError(500, err.Error())
	}
	sRec := apiRecommendationToStructs(aRec)

	args := structs.RecommendationUpsertRequest{
		Recommendation: sRec,
		WriteRequest: structs.WriteRequest{
			Namespace: aRec.Namespace,
		},
	}
	s.parseWriteRequest(req, &args.WriteRequest)
	if sRec.Region == "" {
		sRec.Region = args.Region
	}

	var out structs.SingleRecommendationResponse
	if err := s.agent.RPC("Recommendation.UpsertRecommendation", &args, &out); err != nil {
		return nil, err
	}
	setIndex(resp, out.Index)
	return out.Recommendation, nil
}

func apiRecommendationToStructs(aRec api.Recommendation) *structs.Recommendation {
	sRec := &structs.Recommendation{
		Region:         aRec.Region,
		Namespace:      aRec.Namespace,
		JobID:          aRec.JobID,
		JobVersion:     aRec.JobVersion,
		Group:          aRec.Group,
		Task:           aRec.Task,
		Resource:       aRec.Resource,
		Value:          aRec.Value,
		Current:        aRec.Current,
		Meta:           map[string]interface{}{},
		Stats:          map[string]float64{},
		EnforceVersion: aRec.EnforceVersion,
		CreateIndex:    aRec.CreateIndex,
		ModifyIndex:    aRec.ModifyIndex,
	}
	sRec.ID = aRec.ID
	for k, v := range aRec.Meta {
		sRec.Meta[k] = v
	}
	for k, v := range aRec.Stats {
		sRec.Stats[k] = v
	}
	return sRec
}

func (s *HTTPServer) RecommendationsApplyRequest(resp http.ResponseWriter, req *http.Request) (interface{}, error) {
	if req.Method != "PUT" && req.Method != "POST" {
		return nil, CodedError(405, ErrInvalidMethod)
	}

	var applyRequest api.RecommendationApplyRequest
	if err := decodeBody(req, &applyRequest); err != nil {
		return nil, CodedError(500, err.Error())
	}

	var index uint64
	out := &api.RecommendationApplyResponse{
		UpdatedJobs: []*api.SingleRecommendationApplyResult{},
		Errors:      []*api.SingleRecommendationApplyError{},
	}

	if len(applyRequest.Dismiss)+len(applyRequest.Apply) == 0 {
		return nil, CodedError(400, "apply request must contain one or more recommendations")
	}

	if len(applyRequest.Dismiss) != 0 {
		delArgs := structs.RecommendationDeleteRequest{
			Recommendations: applyRequest.Dismiss,
		}
		s.parseWriteRequest(req, &delArgs.WriteRequest)
		var delOut structs.GenericResponse
		if err := s.agent.RPC("Recommendation.DeleteRecommendations", &delArgs, &delOut); err != nil {
			return nil, err
		}
		index = delOut.Index
	}

	if len(applyRequest.Apply) != 0 {
		applyArgs := structs.RecommendationApplyRequest{
			Recommendations: applyRequest.Apply,
			PolicyOverride:  applyRequest.PolicyOverride,
		}
		s.parseWriteRequest(req, &applyArgs.WriteRequest)
		var applyOut structs.RecommendationApplyResponse
		if err := s.agent.RPC("Recommendation.ApplyRecommendations", &applyArgs, &applyOut); err != nil {
			return nil, err
		}
		index = applyOut.Index
		out = structsRecApplyResultToApiApplyResult(applyOut)
	}

	setIndex(resp, index)
	return out, nil
}

func structsRecApplyResultToApiApplyResult(sApply structs.RecommendationApplyResponse) *api.
	RecommendationApplyResponse {
	aApply := api.RecommendationApplyResponse{
		UpdatedJobs: []*api.SingleRecommendationApplyResult{},
		Errors:      []*api.SingleRecommendationApplyError{},
	}
	for _, u := range sApply.UpdatedJobs {
		aApply.UpdatedJobs = append(aApply.UpdatedJobs, structsRecApplyResultToApi(u))
	}
	for _, e := range sApply.Errors {
		aApply.Errors = append(aApply.Errors, structRecApplyErrorToApi(e))
	}
	return &aApply
}

func structRecApplyErrorToApi(s *structs.SingleRecommendationApplyError) *api.SingleRecommendationApplyError {
	return &api.SingleRecommendationApplyError{
		Namespace:       s.Namespace,
		JobID:           s.JobID,
		Recommendations: s.Recommendations,
		Error:           s.Error,
	}
}

func structsRecApplyResultToApi(s *structs.SingleRecommendationApplyResult) *api.SingleRecommendationApplyResult {
	return &api.SingleRecommendationApplyResult{
		Namespace:       s.Namespace,
		JobID:           s.JobID,
		JobModifyIndex:  s.JobModifyIndex,
		EvalID:          s.EvalID,
		EvalCreateIndex: s.EvalCreateIndex,
		Warnings:        s.Warnings,
		Recommendations: s.Recommendations,
	}
}

func structsRecommendationToApi(sRec *structs.Recommendation) *api.Recommendation {
	aRec := &api.Recommendation{
		ID:             sRec.ID,
		Region:         sRec.Region,
		Namespace:      sRec.Namespace,
		JobID:          sRec.JobID,
		JobVersion:     sRec.JobVersion,
		Group:          sRec.Group,
		Task:           sRec.Task,
		Resource:       sRec.Resource,
		Value:          sRec.Value,
		Current:        sRec.Current,
		Meta:           map[string]interface{}{},
		Stats:          map[string]float64{},
		EnforceVersion: sRec.EnforceVersion,
		CreateIndex:    sRec.CreateIndex,
		ModifyIndex:    sRec.ModifyIndex,
	}
	for k, v := range sRec.Meta {
		aRec.Meta[k] = v
	}
	for k, v := range sRec.Stats {
		aRec.Stats[k] = v
	}
	return aRec
}

func (s *HTTPServer) RecommendationSpecificRequest(resp http.ResponseWriter, req *http.Request) (interface{}, error) {
	id := strings.TrimPrefix(req.URL.Path, "/v1/recommendation/")
	switch req.Method {
	case "GET":
		return s.recQuery(resp, req, id)
	case "DELETE":
		return s.recDelete(resp, req, id)
	default:
		return nil, CodedError(405, ErrInvalidMethod)
	}
}

func (s *HTTPServer) recQuery(resp http.ResponseWriter, req *http.Request,
	recID string) (interface{}, error) {

	args := structs.RecommendationSpecificRequest{
		RecommendationID: recID,
	}
	if s.parse(resp, req, &args.Region, &args.QueryOptions) {
		return nil, nil
	}

	var out structs.SingleRecommendationResponse
	if err := s.agent.RPC("Recommendation.GetRecommendation", &args, &out); err != nil {
		return nil, err
	}
	setMeta(resp, &out.QueryMeta)
	if out.Recommendation == nil {
		return nil, CodedError(404, "Recommendation not found")
	}
	return out.Recommendation, nil
}

func (s *HTTPServer) recDelete(resp http.ResponseWriter, req *http.Request,
	recID string) (interface{}, error) {

	args := structs.RecommendationDeleteRequest{
		Recommendations: []string{recID},
	}
	s.parseWriteRequest(req, &args.WriteRequest)

	var out structs.GenericResponse
	if err := s.agent.RPC("Recommendation.DeleteRecommendations", &args, &out); err != nil {
		return nil, err
	}
	setIndex(resp, out.Index)
	return &out, nil
}
