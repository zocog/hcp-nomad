package api

// Recommendations is used to query the recommendations endpoints.
type Recommendations struct {
	client *Client
}

// Recommendations returns a new handle on the recommendations endpoints.
func (c *Client) Recommendations() *Recommendations {
	return &Recommendations{client: c}
}

// Recommendation is used to serialize a recommendation.
type Recommendation struct {
	ID             *string
	Region         string
	Namespace      string
	JobID          string
	JobVersion     uint64
	Group          string
	Task           string
	Resource       string
	Value          int
	Current        int
	Meta           map[string]interface{}
	Stats          map[string]float64
	EnforceVersion bool

	CreateIndex uint64
	ModifyIndex uint64
}

// RecommendationApplyRequest is used to apply and/or dismiss a set of recommendations
type RecommendationApplyRequest struct {
	Apply          []string
	Dismiss        []string
	PolicyOverride bool
}

// RecommendationApplyResponse is used to apply a set of recommendations
type RecommendationApplyResponse struct {
	UpdatedJobs []*SingleRecommendationApplyResult
	Errors      []*SingleRecommendationApplyError
	WriteMeta
}

type SingleRecommendationApplyResult struct {
	Namespace       string
	JobID           string
	JobModifyIndex  uint64
	EvalID          string
	EvalCreateIndex uint64
	Warnings        string
	Recommendations []string
}

type SingleRecommendationApplyError struct {
	Namespace       string
	JobID           string
	Recommendations []string
	Error           string
}
