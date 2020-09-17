package api

// Recommendations is used to query the namespace endpoints.
type Recommendations struct {
	client *Client
}

// Recommendations returns a new handle on the namespaces.
func (c *Client) Recommendations() *Recommendations {
	return &Recommendations{client: c}
}

// Recommendation is used to serialize a recommendation.
type Recommendation struct {
	ID             string
	Region         string
	Namespace      string
	JobID          string
	JobVersion     uint64
	Group          string
	Task           string
	Resource       string
	Value          int
	Meta           map[string]interface{}
	Stats          map[string]float64
	EnforceVersion bool

	CreateIndex uint64
	ModifyIndex uint64
}
