// +build ent

package audit

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStage_String(t *testing.T) {
	require.Equal(t, "OperationReceived", OperationReceived.String())
	require.Equal(t, "OperationComplete", OperationComplete.String())
	require.Equal(t, "*", AllStages.String())

}

func TestStage_Matches(t *testing.T) {
	require.True(t, AllStages.Matches(OperationComplete))
	require.True(t, AllStages.Matches(OperationReceived))

	require.True(t, OperationReceived.Matches(AllStages))
	require.True(t, OperationComplete.Matches(AllStages))
}

func TestStage_Valid(t *testing.T) {
	cases := []struct {
		s Stage
		e bool
	}{
		{s: OperationComplete, e: true},
		{s: OperationReceived, e: true},
		{s: AllStages, e: true},
		{s: Stage("foo"), e: false},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s expect %v", tc.s, tc.e), func(t *testing.T) {
			require.Equal(t, tc.e, tc.s.Valid())
		})
	}

}
