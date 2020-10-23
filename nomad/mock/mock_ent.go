// +build ent

package mock

import (
	"fmt"

	"github.com/hashicorp/go-licensing"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"

	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/structs"
)

func SentinelPolicy() *structs.SentinelPolicy {
	sp := &structs.SentinelPolicy{
		Name:             fmt.Sprintf("sent-policy-%s", uuid.Generate()),
		Description:      "Super cool policy!",
		EnforcementLevel: "advisory",
		Scope:            "submit-job",
		Policy:           "main = rule { true }",
		CreateIndex:      10,
		ModifyIndex:      20,
	}
	sp.SetHash()
	return sp
}

func QuotaSpec() *structs.QuotaSpec {
	qs := &structs.QuotaSpec{
		Name:        fmt.Sprintf("quota-spec-%s", uuid.Generate()),
		Description: "Super cool quota!",
		Limits: []*structs.QuotaLimit{
			{
				Region: "global",
				RegionLimit: &structs.Resources{
					CPU:      2000,
					MemoryMB: 2000,
				},
			},
			{
				Region: "europe",
				RegionLimit: &structs.Resources{
					CPU:      0,
					MemoryMB: 0,
				},
			},
		},
	}
	qs.SetHash()
	return qs
}

func QuotaUsage() *structs.QuotaUsage {
	qs := QuotaSpec()
	l1 := qs.Limits[0]
	l2 := qs.Limits[1]

	l1.RegionLimit.CPU = 4000
	l1.RegionLimit.MemoryMB = 5000
	l2.RegionLimit.CPU = 40000
	l2.RegionLimit.MemoryMB = 50000
	qs.SetHash()

	qu := &structs.QuotaUsage{
		Name: fmt.Sprintf("quota-usage-%s", uuid.Generate()),
		Used: map[string]*structs.QuotaLimit{
			string(l1.Hash): l1,
			string(l2.Hash): l2,
		},
	}

	return qu
}

func StoredLicense() (*structs.StoredLicense, *licensing.License) {
	license := nomadLicense.NewTestLicense(nomadLicense.TestGovernancePolicyFlags())

	return &structs.StoredLicense{
		Signed:      license.Signed,
		CreateIndex: uint64(1000),
	}, license.License.License
}

func Recommendation(job *structs.Job) *structs.Recommendation {
	rec := &structs.Recommendation{
		ID:         uuid.Generate(),
		Region:     job.Region,
		Namespace:  job.Namespace,
		JobID:      job.ID,
		JobVersion: job.Version,
		Meta: map[string]interface{}{
			"testing": true,
			"mocked":  "also true",
		},
		Stats: map[string]float64{
			"median": 50.0,
			"mean":   51.0,
			"max":    75.5,
			"99":     73.0,
			"min":    0.0,
		},
	}
	rec.Target(
		job.TaskGroups[0].Name,
		job.TaskGroups[0].Tasks[0].Name,
		"CPU")
	rec.Value = job.TaskGroups[0].Tasks[0].Resources.CPU * 2
	rec.Current = job.TaskGroups[0].Tasks[0].Resources.CPU
	return rec
}
