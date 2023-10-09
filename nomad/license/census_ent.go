//go:build ent
// +build ent

package license

import (
	"github.com/hashicorp/go-census/schema"
	census "github.com/hashicorp/go-census/schema"
)

// BillableClientsMetricKey is the metric used to bill nomad customers, it
// represents the number of client nodes.
const BillableClientsMetricKey = "nomad.billable.clients"

func NewCensusSchema() census.Schema {
	return census.Schema{
		Version: "1.0.0",
		Service: "nomad",
		Metrics: []census.Metric{
			{
				Key:         BillableClientsMetricKey,
				Description: "Number of client nodes in the cluster",
				Kind:        schema.Counter,
				Mode:        schema.WriteMode,
			},
		},
	}
}
