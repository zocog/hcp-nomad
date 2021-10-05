//go:build ent
// +build ent

package audit

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStage_String(t *testing.T) {
	t.Parallel()

	require.Equal(t, "OperationReceived", OperationReceived.String())
	require.Equal(t, "OperationComplete", OperationComplete.String())
	require.Equal(t, "*", AllStages.String())

}

func TestStage_Matches(t *testing.T) {
	t.Parallel()

	require.True(t, AllStages.Matches(OperationComplete))
	require.True(t, AllStages.Matches(OperationReceived))

	require.True(t, OperationReceived.Matches(AllStages))
	require.True(t, OperationComplete.Matches(AllStages))
}

func TestStage_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		s     Stage
		valid bool
	}{
		{s: OperationComplete, valid: true},
		{s: OperationReceived, valid: true},
		{s: AllStages, valid: true},
		{s: Stage("*"), valid: true},
		{s: Stage("OperationComplete"), valid: true},
		{s: Stage("OperationReceived"), valid: true},
		{s: Stage("foo"), valid: false},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s expect %v", tc.s, tc.valid), func(t *testing.T) {
			require.Equal(t, tc.valid, tc.s.Valid())
		})
	}

}
