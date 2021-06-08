package nomad

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/nomad/command/agent/consul"
	"github.com/hashicorp/nomad/helper/testlog"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/stretchr/testify/require"
)

func TestConsulACLsAPI_CheckPermissions_ent(t *testing.T) {

	// In Nomad ENT, CheckPermissions will receive the configured group consul
	// namespace parameter. ConsulUsage is built as a map from namespace to
	// associated service/kv reference.

	t.Parallel()

	try := func(t *testing.T, namespace string, usage *structs.ConsulUsage, secretID string, exp error) {
		logger := testlog.HCLogger(t)
		aclAPI := consul.NewMockACLsAPI(logger)
		cAPI := NewConsulACLsAPI(aclAPI, logger, nil)

		err := cAPI.CheckPermissions(context.Background(), namespace, usage, secretID)
		if exp == nil {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
			require.Equal(t, exp.Error(), err.Error())
		}
	}

	t.Run("check-permissions kv read", func(t *testing.T) {
		t.Run("with unset group namespace", func(t *testing.T) {
			t.Run("uses kv has permission from with oss token", func(t *testing.T) {
				u := &structs.ConsulUsage{KV: true}
				try(t, "", u, consul.ExampleOperatorTokenID5, nil)
			})

			t.Run("uses kv without permission with oss token", func(t *testing.T) {
				u := &structs.ConsulUsage{KV: true}
				try(t, "", u, consul.ExampleOperatorTokenID1, errors.New("insufficient Consul ACL permissions to use template"))
			})

			t.Run("uses kv with permission from default token", func(t *testing.T) {
				u := &structs.ConsulUsage{KV: true}
				try(t, "", u, consul.ExampleOperatorTokenID25, nil)
			})

			t.Run("uses kv with incompatible namespace token", func(t *testing.T) {
				u := &structs.ConsulUsage{KV: true}
				try(t, "", u, consul.ExampleOperatorTokenID15, errors.New(`consul ACL token requires using namespace "banana"`))
			})

			t.Run("uses with kv no token", func(t *testing.T) {
				u := &structs.ConsulUsage{KV: true}
				try(t, "", u, "", errors.New("missing consul token"))
			})

			t.Run("uses kv with nonsense token", func(t *testing.T) {
				u := &structs.ConsulUsage{KV: true}
				try(t, "", u, "47d33e22-720a-7fe6-7d7f-418bf844a0be", errors.New("unable to read consul token: no such token"))
			})

			t.Run("unused kv with no token", func(t *testing.T) {
				u := &structs.ConsulUsage{KV: false}
				try(t, "", u, "", nil)
			})
		})

		t.Run("with default group namespace", func(t *testing.T) {
			t.Run("uses kv has permission from default token", func(t *testing.T) {
				u := &structs.ConsulUsage{KV: true}
				try(t, "default", u, consul.ExampleOperatorTokenID25, nil)
			})

			t.Run("uses kv without permission from default token", func(t *testing.T) {
				u := &structs.ConsulUsage{KV: true}
				try(t, "default", u, consul.ExampleOperatorTokenID21, errors.New("insufficient Consul ACL permissions to use template"))
			})

			t.Run("uses kv with permission from incompatible namespace token", func(t *testing.T) {
				u := &structs.ConsulUsage{KV: true}
				try(t, "default", u, consul.ExampleOperatorTokenID15, errors.New(`consul ACL token cannot use namespace "default"`))
			})

			t.Run("uses kv with permission from oss token", func(t *testing.T) {
				u := &structs.ConsulUsage{KV: true}
				try(t, "default", u, consul.ExampleOperatorTokenID5, errors.New(`consul ACL token cannot use namespace "default"`))
			})

			t.Run("uses kv no token", func(t *testing.T) {
				u := &structs.ConsulUsage{KV: true}
				try(t, "default", u, "", errors.New("missing consul token"))
			})

			t.Run("uses kv nonsense token", func(t *testing.T) {
				u := &structs.ConsulUsage{KV: true}
				try(t, "default", u, "47d33e22-720a-7fe6-7d7f-418bf844a0be", errors.New("unable to read consul token: no such token"))
			})

			t.Run("no kv no token", func(t *testing.T) {
				u := &structs.ConsulUsage{KV: false}
				try(t, "default", u, "", nil)
			})
		})

		t.Run("with banana group namespace", func(t *testing.T) {
			t.Run("uses kv has permission", func(t *testing.T) {
				u := &structs.ConsulUsage{KV: true}
				try(t, "banana", u, consul.ExampleOperatorTokenID15, nil)
			})

			t.Run("uses kv without permission", func(t *testing.T) {
				u := &structs.ConsulUsage{KV: true}
				try(t, "banana", u, consul.ExampleOperatorTokenID11, errors.New("insufficient Consul ACL permissions to use template"))
			})

			t.Run("uses kv with permission from incompatible default token", func(t *testing.T) {
				u := &structs.ConsulUsage{KV: true}
				try(t, "banana", u, consul.ExampleOperatorTokenID21, errors.New(`insufficient Consul ACL permissions to use template`))
			})

			t.Run("uses kv with permission from incompatible oss token", func(t *testing.T) {
				u := &structs.ConsulUsage{KV: true}
				try(t, "banana", u, consul.ExampleOperatorTokenID1, errors.New(`consul ACL token cannot use namespace "banana"`))
			})

			t.Run("uses kv no token", func(t *testing.T) {
				u := &structs.ConsulUsage{KV: true}
				try(t, "banana", u, "", errors.New("missing consul token"))
			})

			t.Run("uses kv nonsense token", func(t *testing.T) {
				u := &structs.ConsulUsage{KV: true}
				try(t, "banana", u, "47d33e22-720a-7fe6-7d7f-418bf844a0be", errors.New("unable to read consul token: no such token"))
			})

			t.Run("no kv no token", func(t *testing.T) {
				u := &structs.ConsulUsage{KV: false}
				try(t, "banana", u, "", nil)
			})
		})
	})

	t.Run("check-permissions service write", func(t *testing.T) {
		usage := &structs.ConsulUsage{Services: []string{"service1"}}

		t.Run("with unset group namespace", func(t *testing.T) {
			t.Run("operator has service write with oss token", func(t *testing.T) {
				try(t, "", usage, consul.ExampleOperatorTokenID1, nil)
			})

			t.Run("operator has service write with default token", func(t *testing.T) {
				try(t, "", usage, consul.ExampleOperatorTokenID21, nil)
			})

			t.Run("operator has service write with incompatible namespace token", func(t *testing.T) {
				try(t, "", usage, consul.ExampleOperatorTokenID11, errors.New(`consul ACL token requires using namespace "banana"`))
			})

			t.Run("operator has service_prefix write with oss token", func(t *testing.T) {
				u := &structs.ConsulUsage{Services: []string{"foo-service1"}}
				try(t, "", u, consul.ExampleOperatorTokenID2, nil)
			})

			t.Run("operator has service_prefix write with default token", func(t *testing.T) {
				u := &structs.ConsulUsage{Services: []string{"foo-service1"}}
				try(t, "", u, consul.ExampleOperatorTokenID22, nil)
			})

			t.Run("operator has service_prefix write with incompatible namespace token", func(t *testing.T) {
				u := &structs.ConsulUsage{Services: []string{"foo-service1"}}
				try(t, "", u, consul.ExampleOperatorTokenID12, errors.New(`consul ACL token requires using namespace "banana"`))
			})

			t.Run("operator has service_prefix write wrong prefix", func(t *testing.T) {
				u := &structs.ConsulUsage{Services: []string{"bar-service1"}}
				try(t, "", u, consul.ExampleOperatorTokenID2, errors.New(`insufficient Consul ACL permissions to write service "bar-service1"`))
			})

			t.Run("operator permissions insufficient", func(t *testing.T) {
				try(t, "", usage, consul.ExampleOperatorTokenID3, errors.New(`insufficient Consul ACL permissions to write service "service1"`))
			})

			t.Run("operator provided no token", func(t *testing.T) {
				try(t, "", usage, "", errors.New("missing consul token"))
			})

			t.Run("operator provided nonsense token", func(t *testing.T) {
				try(t, "", usage, "f1682bde-1e71-90b1-9204-85d35467ba61", errors.New("unable to read consul token: no such token"))
			})
		})

		t.Run("with default group namespace", func(t *testing.T) {
			t.Run("operator has service write with default token", func(t *testing.T) {
				try(t, "default", usage, consul.ExampleOperatorTokenID21, nil)
			})

			t.Run("operator has service write with incompatible oss token", func(t *testing.T) {
				try(t, "default", usage, consul.ExampleOperatorTokenID1, errors.New(`consul ACL token cannot use namespace "default"`))
			})

			t.Run("operator has service write with incompatible namespace token", func(t *testing.T) {
				try(t, "default", usage, consul.ExampleOperatorTokenID11, errors.New(`consul ACL token cannot use namespace "default"`))
			})

			t.Run("operator has service_prefix write", func(t *testing.T) {
				u := &structs.ConsulUsage{Services: []string{"foo-service1"}}
				try(t, "default", u, consul.ExampleOperatorTokenID22, nil)
			})

			t.Run("operator has service_prefix write with incompatible oss token", func(t *testing.T) {
				u := &structs.ConsulUsage{Services: []string{"foo-service1"}}
				try(t, "default", u, consul.ExampleOperatorTokenID2, errors.New(`consul ACL token cannot use namespace "default"`))
			})

			t.Run("operator has service_prefix write with incompatible namespace token", func(t *testing.T) {
				u := &structs.ConsulUsage{Services: []string{"foo-service1"}}
				try(t, "default", u, consul.ExampleOperatorTokenID12, errors.New(`consul ACL token cannot use namespace "default"`))
			})

			t.Run("operator has service_prefix write wrong prefix", func(t *testing.T) {
				u := &structs.ConsulUsage{Services: []string{"bar-service1"}}
				try(t, "default", u, consul.ExampleOperatorTokenID22, errors.New(`insufficient Consul ACL permissions to write service "bar-service1"`))
			})

			t.Run("operator permissions insufficient", func(t *testing.T) {
				try(t, "default", usage, consul.ExampleOperatorTokenID23, errors.New(`insufficient Consul ACL permissions to write service "service1"`))
			})

			t.Run("operator provided no token", func(t *testing.T) {
				try(t, "default", usage, "", errors.New("missing consul token"))
			})

			t.Run("operator provided nonsense token", func(t *testing.T) {
				try(t, "default", usage, "f1682bde-1e71-90b1-9204-85d35467ba61", errors.New("unable to read consul token: no such token"))
			})
		})

		t.Run("with banana group namespace", func(t *testing.T) {
			t.Run("operator has service write with banana token", func(t *testing.T) {
				try(t, "banana", usage, consul.ExampleOperatorTokenID11, nil)
			})

			t.Run("operator has service write with incompatible oss token", func(t *testing.T) {
				try(t, "banana", usage, consul.ExampleOperatorTokenID1, errors.New(`consul ACL token cannot use namespace "banana"`))
			})

			t.Run("operator has service write with incompatible default token", func(t *testing.T) {
				try(t, "banana", usage, consul.ExampleOperatorTokenID21, errors.New(`insufficient Consul ACL permissions to write service "service1"`))
			})

			t.Run("operator has service_prefix write", func(t *testing.T) {
				u := &structs.ConsulUsage{Services: []string{"foo-service1"}}
				try(t, "banana", u, consul.ExampleOperatorTokenID12, nil)
			})

			t.Run("operator has service_prefix write with incompatible oss token", func(t *testing.T) {
				u := &structs.ConsulUsage{Services: []string{"foo-service1"}}
				try(t, "banana", u, consul.ExampleOperatorTokenID2, errors.New(`consul ACL token cannot use namespace "banana"`))
			})

			t.Run("operator has service_prefix write with incompatible default token", func(t *testing.T) {
				u := &structs.ConsulUsage{Services: []string{"foo-service1"}}
				try(t, "banana", u, consul.ExampleOperatorTokenID22, errors.New(`insufficient Consul ACL permissions to write service "foo-service1"`))
			})

			t.Run("operator has service_prefix write wrong prefix", func(t *testing.T) {
				u := &structs.ConsulUsage{Services: []string{"bar-service1"}}
				try(t, "banana", u, consul.ExampleOperatorTokenID12, errors.New(`insufficient Consul ACL permissions to write service "bar-service1"`))
			})

			t.Run("operator permissions insufficient", func(t *testing.T) {
				try(t, "banana", usage, consul.ExampleOperatorTokenID13, errors.New(`insufficient Consul ACL permissions to write service "service1"`))
			})

			t.Run("operator provided no token", func(t *testing.T) {
				try(t, "banana", usage, "", errors.New("missing consul token"))
			})

			t.Run("operator provided nonsense token", func(t *testing.T) {
				try(t, "banana", usage, "f1682bde-1e71-90b1-9204-85d35467ba61", errors.New("unable to read consul token: no such token"))
			})
		})
	})

	t.Run("check-permissions connect service identity write", func(t *testing.T) {
		usage := &structs.ConsulUsage{Kinds: []structs.TaskKind{structs.NewTaskKind(structs.ConnectProxyPrefix, "service1")}}

		t.Run("with unset group namespace", func(t *testing.T) {
			t.Run("operator has service write with oss token", func(t *testing.T) {
				try(t, "", usage, consul.ExampleOperatorTokenID1, nil)
			})

			t.Run("operator has service write with incompatible namespace token", func(t *testing.T) {
				try(t, "", usage, consul.ExampleOperatorTokenID11, errors.New(`consul ACL token requires using namespace "banana"`))
			})

			t.Run("operator has service write with default token", func(t *testing.T) {
				try(t, "", usage, consul.ExampleOperatorTokenID21, nil)
			})

			t.Run("operator has service_prefix write with oss token", func(t *testing.T) {
				u := &structs.ConsulUsage{Kinds: []structs.TaskKind{structs.NewTaskKind(structs.ConnectProxyPrefix, "foo-service1")}}
				try(t, "", u, consul.ExampleOperatorTokenID2, nil)
			})

			t.Run("operator has service_prefix write with incompatible namespace token", func(t *testing.T) {
				u := &structs.ConsulUsage{Kinds: []structs.TaskKind{structs.NewTaskKind(structs.ConnectProxyPrefix, "foo-service1")}}
				try(t, "", u, consul.ExampleOperatorTokenID12, errors.New(`consul ACL token requires using namespace "banana"`))
			})

			t.Run("operator has service_prefix write with default namespace token", func(t *testing.T) {
				u := &structs.ConsulUsage{Kinds: []structs.TaskKind{structs.NewTaskKind(structs.ConnectProxyPrefix, "foo-service1")}}
				try(t, "", u, consul.ExampleOperatorTokenID22, nil)
			})

			t.Run("operator has service_prefix write wrong prefix", func(t *testing.T) {
				u := &structs.ConsulUsage{Kinds: []structs.TaskKind{structs.NewTaskKind(structs.ConnectProxyPrefix, "bar-service1")}}
				try(t, "", u, consul.ExampleOperatorTokenID2, errors.New(`insufficient Consul ACL permissions to write Connect service "bar-service1"`))
			})

			t.Run("operator permissions insufficient", func(t *testing.T) {
				try(t, "", usage, consul.ExampleOperatorTokenID3, errors.New(`insufficient Consul ACL permissions to write Connect service "service1"`))
			})

			t.Run("operator provided no token", func(t *testing.T) {
				try(t, "", usage, "", errors.New("missing consul token"))
			})

			t.Run("operator provided nonsense token", func(t *testing.T) {
				try(t, "", usage, "f1682bde-1e71-90b1-9204-85d35467ba61", errors.New("unable to read consul token: no such token"))
			})
		})

		t.Run("with default group namespace", func(t *testing.T) {
			t.Run("operator has service write with default token", func(t *testing.T) {
				try(t, "default", usage, consul.ExampleOperatorTokenID21, nil)
			})

			t.Run("operator has service write with incompatible oss token", func(t *testing.T) {
				try(t, "default", usage, consul.ExampleOperatorTokenID1, errors.New(`consul ACL token cannot use namespace "default"`))
			})

			t.Run("operator has service write with incompatible namespace token", func(t *testing.T) {
				try(t, "default", usage, consul.ExampleOperatorTokenID11, errors.New(`consul ACL token cannot use namespace "default"`))
			})

			t.Run("operator has service_prefix write with default token", func(t *testing.T) {
				u := &structs.ConsulUsage{Kinds: []structs.TaskKind{structs.NewTaskKind(structs.ConnectProxyPrefix, "foo-service1")}}
				try(t, "default", u, consul.ExampleOperatorTokenID22, nil)
			})

			t.Run("operator has service_prefix write with incompatible oss token", func(t *testing.T) {
				u := &structs.ConsulUsage{Kinds: []structs.TaskKind{structs.NewTaskKind(structs.ConnectProxyPrefix, "foo-service1")}}
				try(t, "default", u, consul.ExampleOperatorTokenID2, errors.New(`consul ACL token cannot use namespace "default"`))
			})

			t.Run("operator has service_prefix write with incompatible namespace token", func(t *testing.T) {
				u := &structs.ConsulUsage{Kinds: []structs.TaskKind{structs.NewTaskKind(structs.ConnectProxyPrefix, "foo-service1")}}
				try(t, "default", u, consul.ExampleOperatorTokenID12, errors.New(`consul ACL token cannot use namespace "default"`))
			})

			t.Run("operator has service_prefix write wrong prefix", func(t *testing.T) {
				u := &structs.ConsulUsage{Kinds: []structs.TaskKind{structs.NewTaskKind(structs.ConnectProxyPrefix, "bar-service1")}}
				try(t, "default", u, consul.ExampleOperatorTokenID22, errors.New(`insufficient Consul ACL permissions to write Connect service "bar-service1"`))
			})

			t.Run("operator permissions insufficient", func(t *testing.T) {
				try(t, "default", usage, consul.ExampleOperatorTokenID23, errors.New(`insufficient Consul ACL permissions to write Connect service "service1"`))
			})

			t.Run("operator provided no token", func(t *testing.T) {
				try(t, "default", usage, "", errors.New("missing consul token"))
			})

			t.Run("operator provided nonsense token", func(t *testing.T) {
				try(t, "default", usage, "f1682bde-1e71-90b1-9204-85d35467ba61", errors.New("unable to read consul token: no such token"))
			})
		})

		t.Run("with banana group namespace", func(t *testing.T) {
			t.Run("operator has service write with banana token", func(t *testing.T) {
				try(t, "banana", usage, consul.ExampleOperatorTokenID11, nil)
			})

			t.Run("operator has service write with incompatible oss token", func(t *testing.T) {
				try(t, "banana", usage, consul.ExampleOperatorTokenID1, errors.New(`consul ACL token cannot use namespace "banana"`))
			})

			t.Run("operator has service write with incompatible default token", func(t *testing.T) {
				try(t, "banana", usage, consul.ExampleOperatorTokenID21, errors.New(`insufficient Consul ACL permissions to write Connect service "service1"`))
			})

			t.Run("operator has service_prefix write with banana token", func(t *testing.T) {
				u := &structs.ConsulUsage{Kinds: []structs.TaskKind{structs.NewTaskKind(structs.ConnectProxyPrefix, "foo-service1")}}
				try(t, "banana", u, consul.ExampleOperatorTokenID12, nil)
			})

			t.Run("operator has service_prefix write with incompatible oss token", func(t *testing.T) {
				u := &structs.ConsulUsage{Kinds: []structs.TaskKind{structs.NewTaskKind(structs.ConnectProxyPrefix, "foo-service1")}}
				try(t, "banana", u, consul.ExampleOperatorTokenID2, errors.New(`consul ACL token cannot use namespace "banana"`))
			})

			t.Run("operator has service_prefix write with incompatible default token", func(t *testing.T) {
				u := &structs.ConsulUsage{Kinds: []structs.TaskKind{structs.NewTaskKind(structs.ConnectProxyPrefix, "foo-service1")}}
				try(t, "banana", u, consul.ExampleOperatorTokenID22, errors.New(`insufficient Consul ACL permissions to write Connect service "foo-service1"`))
			})

			t.Run("operator has service_prefix write wrong prefix", func(t *testing.T) {
				u := &structs.ConsulUsage{Kinds: []structs.TaskKind{structs.NewTaskKind(structs.ConnectProxyPrefix, "bar-service1")}}
				try(t, "banana", u, consul.ExampleOperatorTokenID12, errors.New(`insufficient Consul ACL permissions to write Connect service "bar-service1"`))
			})

			t.Run("operator permissions insufficient", func(t *testing.T) {
				try(t, "banana", usage, consul.ExampleOperatorTokenID13, errors.New(`insufficient Consul ACL permissions to write Connect service "service1"`))
			})

			t.Run("operator provided no token", func(t *testing.T) {
				try(t, "banana", usage, "", errors.New("missing consul token"))
			})

			t.Run("operator provided nonsense token", func(t *testing.T) {
				try(t, "banana", usage, "f1682bde-1e71-90b1-9204-85d35467ba61", errors.New("unable to read consul token: no such token"))
			})
		})
	})
}
