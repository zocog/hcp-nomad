//go:build ent
// +build ent

package reporting

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/helper/pointer"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/nomad/structs/config"
	"github.com/shoenig/test/must"
)

type mockClientCounter struct{}

func (mcg *mockClientCounter) GetClientNodesCount() (int, error) {
	return 5, nil
}

func TestGenerateULIDFromMetadata(t *testing.T) {

	testTime, _ := time.Parse("01/02/2006", "04/04/1985")
	unixTT := testTime.UnixNano()

	testCases := []struct {
		uuID string
		ulID string
	}{
		{
			"67bc5dfd-4027-8f6e-13b5-1c20c1153733",
			"00E0BEMW00ZWK85V2SXRA9QQT3",
		},
		{
			"8f7a2c6d-5aca-9835-6c2d-57a01c567f51",
			"00E0BEMW00EN9HQTWF3WTDVJR9",
		},
		{
			"f8393f5f-462d-b826-d250-5f45e0727237",
			"00E0BEMW008F7AZY03QX39FJ8K",
		},
		{
			uuid.Nil.String(),
			"00E0BEMW00KY4WGJJNKXBKCDN4",
		},
	}

	for _, tc := range testCases {
		res, err := generateULIDFromMetadata(structs.ClusterMetadata{
			ClusterID:  tc.uuID,
			CreateTime: unixTT,
		})
		must.NoError(t, err)
		must.StrContains(t, tc.ulID, res)
	}
}

func TestReportingManager_Disabled(t *testing.T) {
	tm, err := NewManager(hclog.NewNullLogger(),
		&config.ReportingConfig{
			License: &config.LicenseReportingConfig{
				Enabled: pointer.Of(false),
			},
		},
		&license.License{},
		&mockClientCounter{})

	must.NoError(t, err)

	sc := make(chan struct{})
	err = tm.Start(sc, structs.ClusterMetadata{
		ClusterID:  "clusterID",
		CreateTime: time.Now().UnixNano(),
	})

	must.NoError(t, err)
	must.Nil(t, tm.census)
	must.False(t, tm.enabled)
}

func TestReportingManager_MissingClusterIDError(t *testing.T) {
	tm, err := NewManager(hclog.NewNullLogger(),
		&config.ReportingConfig{
			License: &config.LicenseReportingConfig{
				Enabled: pointer.Of(true),
			},
		},
		&license.License{},
		&mockClientCounter{})

	must.NoError(t, err)

	sc := make(chan struct{})
	err = tm.Start(sc, structs.ClusterMetadata{
		ClusterID:  "",
		CreateTime: time.Now().UnixNano(),
	})

	must.Error(t, err)
}
