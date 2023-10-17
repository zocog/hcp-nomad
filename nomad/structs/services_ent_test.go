//go:build ent
// +build ent

package structs

import (
	"testing"

	"github.com/shoenig/test/must"
)

func TestService_GetConsulCluster(t *testing.T) {
	tg1 := &TaskGroup{
		Consul: &Consul{Cluster: "consul-foo0"},
		Tasks: []*Task{{
			Name:   "task1",
			Consul: &Consul{Cluster: "consul-foo1"},
			Services: []*Service{
				{
					Name:     "tg1-group-svc-1",
					TaskName: "task1",
					Provider: ServiceProviderConsul,
					Cluster:  "consul-foo2",
				},
				{
					Name:     "tg1-group-svc-1",
					TaskName: "task1",
					Provider: ServiceProviderConsul,
				},
			},
		}},
		Services: []*Service{
			{
				Name:     "tg1-group-svc-1",
				Provider: ServiceProviderConsul,
				Cluster:  "consul-foo3",
			},
			{
				Name:     "tg1-group-svc-2",
				Provider: ServiceProviderConsul,
			},
			{
				Name:     "tg1-group-svc-3",
				TaskName: "task1",
				Provider: ServiceProviderConsul,
				Cluster:  "consul-foo4",
			},
			{
				Name:     "tg1-group-svc-4",
				TaskName: "task1",
				Provider: ServiceProviderConsul,
			},
		},
	}

	must.Eq(t, "consul-foo2", tg1.Tasks[0].Services[0].GetConsulClusterName(tg1))
	must.Eq(t, "consul-foo1", tg1.Tasks[0].Services[1].GetConsulClusterName(tg1))
	must.Eq(t, "consul-foo3", tg1.Services[0].GetConsulClusterName(tg1))
	must.Eq(t, "consul-foo0", tg1.Services[1].GetConsulClusterName(tg1))
	must.Eq(t, "consul-foo4", tg1.Services[2].GetConsulClusterName(tg1))
	must.Eq(t, "consul-foo1", tg1.Services[3].GetConsulClusterName(tg1))

	tg2 := &TaskGroup{
		Tasks: []*Task{{
			Name: "task2",
			Services: []*Service{
				{
					Name:     "tg2-group-svc-1",
					TaskName: "task2",
					Provider: ServiceProviderConsul,
				},
			},
		}},
		Services: []*Service{
			{
				Name:     "tg2-group-svc-1",
				Provider: ServiceProviderConsul,
			},
			{
				Name:     "tg2-group-svc-2",
				TaskName: "task2",
				Provider: ServiceProviderConsul,
			},
		},
	}

	must.Eq(t, "default", tg2.Tasks[0].Services[0].GetConsulClusterName(tg2))
	must.Eq(t, "default", tg2.Services[0].GetConsulClusterName(tg2))
	must.Eq(t, "default", tg2.Services[1].GetConsulClusterName(tg2))
}
