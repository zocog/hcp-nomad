//go:build ent
// +build ent

package nomad

import (
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/nomad/command/agent/consul"
	"github.com/hashicorp/nomad/helper/testlog"
	"github.com/stretchr/testify/require"
)

func TestConsulACLsAPI_hasSufficientPolicy_ent(t *testing.T) {
	t.Parallel()

	try := func(t *testing.T, namespace, task string, token *api.ACLToken, exp bool) {
		logger := testlog.HCLogger(t)
		cAPI := &consulACLsAPI{
			aclClient: consul.NewMockACLsAPI(logger),
			logger:    logger,
		}
		result, err := cAPI.canWriteService(namespace, task, token)
		require.NoError(t, err)
		require.Equal(t, exp, result)
	}

	// In Nomad ENT, group consul namespace is respected.

	t.Run("no namespace with oss token", func(t *testing.T) {
		t.Run("no useful policy or role", func(t *testing.T) {
			try(t, "", "service1", consul.ExampleOperatorToken0, false)
		})

		t.Run("working policy only", func(t *testing.T) {
			try(t, "", "service1", consul.ExampleOperatorToken1, true)
		})

		t.Run("working role only", func(t *testing.T) {
			try(t, "", "service1", consul.ExampleOperatorToken4, true)
		})
	})

	t.Run("default namespace with default token", func(t *testing.T) {
		t.Run("no useful policy or role", func(t *testing.T) {
			try(t, "default", "service1", consul.ExampleOperatorToken20, false)
		})

		t.Run("working policy only", func(t *testing.T) {
			try(t, "default", "service1", consul.ExampleOperatorToken21, true)
		})

		t.Run("working role only", func(t *testing.T) {
			try(t, "default", "service1", consul.ExampleOperatorToken24, true)
		})
	})

	t.Run("banana namespace with banana token", func(t *testing.T) {
		t.Run("no useful policy or role", func(t *testing.T) {
			try(t, "banana", "service1", consul.ExampleOperatorToken10, false)
		})

		t.Run("working policy only", func(t *testing.T) {
			try(t, "banana", "service1", consul.ExampleOperatorToken11, true)
		})

		t.Run("working role only", func(t *testing.T) {
			try(t, "banana", "service1", consul.ExampleOperatorToken14, true)
		})
	})

	t.Run("default namespace with banana token", func(t *testing.T) {
		t.Run("no useful policy or role", func(t *testing.T) {
			try(t, "default", "service1", consul.ExampleOperatorToken10, false)
		})

		t.Run("working policy only", func(t *testing.T) {
			try(t, "default", "service1", consul.ExampleOperatorToken11, false)
		})

		t.Run("working role only", func(t *testing.T) {
			try(t, "default", "service1", consul.ExampleOperatorToken14, false)
		})
	})

	t.Run("banana namespace with default token", func(t *testing.T) {
		t.Run("no useful policy or role", func(t *testing.T) {
			try(t, "banana", "service1", consul.ExampleOperatorToken20, false)
		})

		t.Run("working policy only", func(t *testing.T) {
			try(t, "banana", "service1", consul.ExampleOperatorToken21, false)
		})

		t.Run("working role only", func(t *testing.T) {
			try(t, "banana", "service1", consul.ExampleOperatorToken24, false)
		})
	})
}
