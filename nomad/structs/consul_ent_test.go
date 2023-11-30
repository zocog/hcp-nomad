//go:build ent
// +build ent

package structs

import (
	"testing"

	"github.com/hashicorp/nomad/ci"
	"github.com/shoenig/test/must"
)

func TestJob_ConfigEntries(t *testing.T) {
	ci.Parallel(t)

	ingress := &ConsulConnect{
		Gateway: &ConsulGateway{
			Ingress: new(ConsulIngressConfigEntry),
		},
	}

	terminating := &ConsulConnect{
		Gateway: &ConsulGateway{
			Terminating: new(ConsulTerminatingConfigEntry),
		},
	}

	j := &Job{
		TaskGroups: []*TaskGroup{{
			Name:   "group1",
			Consul: nil,
			Services: []*Service{{
				Name:    "group1-service1",
				Connect: ingress,
			}, {
				Name:    "group1-service2",
				Connect: nil,
			}, {
				Name:    "group1-service3",
				Connect: terminating,
			}},
		}, {
			Name:   "group2",
			Consul: nil,
			Services: []*Service{{
				Name:    "group2-service1",
				Connect: ingress,
			}},
		}, {
			Name:   "group3",
			Consul: &Consul{Namespace: "apple"},
			Services: []*Service{{
				Name:    "group3-service1",
				Connect: ingress,
			}},
		}, {
			Name:   "group4",
			Consul: &Consul{Namespace: "apple"},
			Services: []*Service{{
				Name:    "group4-service1",
				Connect: ingress,
			}, {
				Name:    "group4-service2",
				Connect: terminating,
			}},
		}, {
			Name:   "group5",
			Consul: &Consul{Namespace: "banana"},
			Services: []*Service{{
				Name:    "group5-service1",
				Connect: ingress,
			}},
		}},
	}

	exp := map[string]*ConsulConfigEntries{

		// empty string is used for unset namespace from GetNamespace
		"": {
			Ingress: map[string]*ConsulIngressConfigEntry{
				"group1-service1": new(ConsulIngressConfigEntry),
				"group2-service1": new(ConsulIngressConfigEntry),
			},
			Terminating: map[string]*ConsulTerminatingConfigEntry{
				"group1-service3": new(ConsulTerminatingConfigEntry),
			},
		},
		"apple": {
			Ingress: map[string]*ConsulIngressConfigEntry{
				"group3-service1": new(ConsulIngressConfigEntry),
				"group4-service1": new(ConsulIngressConfigEntry),
			},
			Terminating: map[string]*ConsulTerminatingConfigEntry{
				"group4-service2": new(ConsulTerminatingConfigEntry),
			},
		},
		"banana": {
			Ingress: map[string]*ConsulIngressConfigEntry{
				"group5-service1": new(ConsulIngressConfigEntry),
			},
			Terminating: map[string]*ConsulTerminatingConfigEntry{
				// empty
			},
		},
	}

	entries := j.ConfigEntries()
	must.Eq(t, exp, entries)
}

func TestTask_GetConsulCluster(t *testing.T) {
	tg1 := &TaskGroup{
		Consul: &Consul{Cluster: "consul-foo0"},
		Tasks: []*Task{
			{
				Name:   "task1",
				Consul: &Consul{Cluster: "consul-foo1"},
			},
			{
				Name: "task2",
			},
		},
	}

	must.Eq(t, "consul-foo1", tg1.Tasks[0].GetConsulClusterName(tg1))
	must.Eq(t, "consul-foo0", tg1.Tasks[1].GetConsulClusterName(tg1))

	tg2 := &TaskGroup{
		Tasks: []*Task{{
			Name: "task2",
		}},
	}

	must.Eq(t, "default", tg2.Tasks[0].GetConsulClusterName(tg2))
}

func TestConsul_Usages(t *testing.T) {
	ci.Parallel(t)

	job := &Job{
		ConsulNamespace: "default",
		TaskGroups: []*TaskGroup{
			{
				Consul: &Consul{Namespace: "nondefault1"},
				Services: []*Service{
					{Name: "service1"},
					{Name: "nomad1", Provider: "nomad"},
				},
				Tasks: []*Task{
					{
						Consul: &Consul{Namespace: "nondefault2"},
						Services: []*Service{
							{Name: "service2"},
							{Name: "nomad2", Provider: "nomad"},
						},
					},
					{
						Services: []*Service{
							{Name: "service3"},
							{Name: "nomad3", Provider: "nomad"},
						},
					},
				},
			},
			{
				Services: []*Service{
					{Name: "service4"},
					{Name: "nomad4", Provider: "nomad"},
				},
				Tasks: []*Task{
					{
						Services: []*Service{
							{Name: "service5"},
							{Name: "nomad5", Provider: "nomad"},
						},
						Templates: []*Template{{}},
					},
				},
			},
		},
	}

	usages := job.ConsulUsages()
	must.Eq(t,
		map[string]*ConsulUsage{
			"default": {
				Services: []string{"service4", "service5"},
				KV:       true,
			},
			"nondefault1": {
				Services: []string{"service1", "service3"},
				KV:       false,
			},
			"nondefault2": {
				Services: []string{"service2"},
				KV:       false,
			},
		},
		usages)

}
