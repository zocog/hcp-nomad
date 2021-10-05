//go:build ent || consulent
// +build ent consulent

package nomad

import (
	"testing"
	"time"

	"github.com/hashicorp/consul/agent/consul/autopilot"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/nomad/testutil"
	"github.com/hashicorp/raft"
	"github.com/stretchr/testify/assert"
)

func TestAdvancedAutopilot_DesignateNonVoter(t *testing.T) {
	assert := assert.New(t)
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 2
		c.RaftConfig.ProtocolVersion = 3
	})
	defer cleanupS1()

	s2, cleanupS2 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 2
		c.RaftConfig.ProtocolVersion = 3
	})
	defer cleanupS2()

	s3, cleanupS3 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 2
		c.NonVoter = true
		c.RaftConfig.ProtocolVersion = 3
	})
	defer cleanupS3()

	TestJoin(t, s1, s2, s3)

	testutil.WaitForLeader(t, s1.RPC)
	retry.Run(t, func(r *retry.R) { r.Check(wantRaft([]*Server{s1, s2, s3})) })

	// Wait twice the server stabilization threshold to give the server
	// time to be promoted
	time.Sleep(2 * s1.config.AutopilotConfig.ServerStabilizationTime)

	future := s1.raft.GetConfiguration()
	assert.Nil(future.Error())

	servers := future.Configuration().Servers

	// s2 should be a voter
	if !autopilot.IsPotentialVoter(findServer(t, servers, s2).Suffrage) {
		t.Fatalf("bad: %v", servers)
	}

	// s3 should remain a non-voter
	if autopilot.IsPotentialVoter(findServer(t, servers, s3).Suffrage) {
		t.Fatalf("bad: %v", servers)
	}
}

func TestAdvancedAutopilot_RedundancyZone(t *testing.T) {
	assert := assert.New(t)
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 3
		c.RaftConfig.ProtocolVersion = 3
		c.AutopilotConfig.EnableRedundancyZones = true
		c.RedundancyZone = "east"
	})
	defer cleanupS1()

	s2, cleanupS2 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 3
		c.RaftConfig.ProtocolVersion = 3
		c.AutopilotConfig.EnableRedundancyZones = true
		c.RedundancyZone = "west"
	})
	defer cleanupS2()

	s3, cleanupS3 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 3
		c.RaftConfig.ProtocolVersion = 3
		c.AutopilotConfig.EnableRedundancyZones = true
		c.RedundancyZone = "west-2"
	})
	defer cleanupS3()

	TestJoin(t, s1, s2, s3)
	testutil.WaitForLeader(t, s1.RPC)
	retry.Run(t, func(r *retry.R) { r.Check(wantRaft([]*Server{s1, s2, s3})) })

	// Wait past the stabilization time to give the servers a chance to be promoted
	time.Sleep(2*s1.config.AutopilotConfig.ServerStabilizationTime + s1.config.AutopilotInterval)

	// Now s2 and s3 should be voters
	{
		future := s1.raft.GetConfiguration()
		assert.Nil(future.Error())
		servers := future.Configuration().Servers
		assert.Equal(3, len(servers))
		// s2 and s3 should be voters now
		assert.Equal(raft.Voter, findServer(t, servers, s2).Suffrage)
		assert.Equal(raft.Voter, findServer(t, servers, s3).Suffrage)
	}

	// Join s4
	s4, cleanupS4 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 0
		c.RaftConfig.ProtocolVersion = 3
		c.AutopilotConfig.EnableRedundancyZones = true
		c.RedundancyZone = "west-2"
	})
	defer cleanupS4()

	TestJoin(t, s1, s4)
	t.Log("before sleeping")
	time.Sleep(2*s1.config.AutopilotConfig.ServerStabilizationTime + s1.config.AutopilotInterval)

	// s4 should not be a voter yet
	{
		future := s1.raft.GetConfiguration()
		assert.Nil(future.Error())
		servers := future.Configuration().Servers
		assert.Equal(raft.Nonvoter, findServer(t, servers, s4).Suffrage)
	}

	s3.Shutdown()

	// s4 should be a voter now, s3 should be removed
	retry.Run(t, func(r *retry.R) {
		r.Check(wantRaft([]*Server{s1, s2, s4}))
		future := s1.raft.GetConfiguration()
		if err := future.Error(); err != nil {
			r.Fatal(err)
		}
		servers := future.Configuration().Servers
		for _, s := range servers {
			if s.Suffrage != raft.Voter {
				r.Fatalf("bad: %v", servers)
			}
		}
	})
}

func TestAdvancedAutopilot_UpgradeMigration(t *testing.T) {
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 2
		c.RaftConfig.ProtocolVersion = 3
		c.Build = "0.8.0"
	})
	defer cleanupS1()

	s2, cleanupS2 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 2
		c.RaftConfig.ProtocolVersion = 3
		c.Build = "0.8.1"
	})
	defer cleanupS2()

	TestJoin(t, s1, s2)
	testutil.WaitForLeader(t, s1.RPC)

	// Wait for the migration to complete
	retry.Run(t, func(r *retry.R) {
		future := s1.raft.GetConfiguration()
		if err := future.Error(); err != nil {
			r.Fatal(err)
		}
		for _, s := range future.Configuration().Servers {
			switch s.ID {
			case raft.ServerID(s1.config.NodeID):
				if got, want := s.Suffrage, raft.Nonvoter; got != want {
					r.Fatalf("%v got %v want %v", s.ID, got, want)
				}

			case raft.ServerID(s2.config.NodeID):
				if got, want := s.Suffrage, raft.Voter; got != want {
					r.Fatalf("%v got %v want %v", s.ID, got, want)
				}

			default:
				r.Fatalf("unexpected server %s", s.ID)
			}
		}

		if !s2.IsLeader() {
			r.Fatal("server 2 should be the leader")
		}
	})
}

func TestAdvancedAutopilot_CustomUpgradeMigration(t *testing.T) {
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 2
		c.RaftConfig.ProtocolVersion = 3
		c.AutopilotConfig.EnableCustomUpgrades = true
		c.UpgradeVersion = "0.0.1"
	})
	defer cleanupS1()

	s2, cleanupS2 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 2
		c.RaftConfig.ProtocolVersion = 3
		c.AutopilotConfig.EnableCustomUpgrades = true
		c.UpgradeVersion = "0.0.2"
	})
	defer cleanupS2()

	TestJoin(t, s1, s2)
	testutil.WaitForLeader(t, s1.RPC)

	// Wait for the migration to complete
	retry.Run(t, func(r *retry.R) {
		future := s1.raft.GetConfiguration()
		if err := future.Error(); err != nil {
			r.Fatal(err)
		}
		for _, s := range future.Configuration().Servers {
			switch s.ID {
			case raft.ServerID(s1.config.NodeID):
				if got, want := s.Suffrage, raft.Nonvoter; got != want {
					r.Fatalf("%v got %v want %v", s.ID, got, want)
				}

			case raft.ServerID(s2.config.NodeID):
				if got, want := s.Suffrage, raft.Voter; got != want {
					r.Fatalf("%v got %v want %v", s.ID, got, want)
				}

			default:
				r.Fatalf("unexpected server %s", s.ID)
			}
		}

		if !s2.IsLeader() {
			r.Fatal("server 2 should be the leader")
		}
	})
}

func TestAdvancedAutopilot_DisableUpgradeMigration(t *testing.T) {
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.RaftConfig.ProtocolVersion = 3
		c.AutopilotConfig.DisableUpgradeMigration = true
		c.Build = "0.8.0"
	})
	defer cleanupS1()

	testutil.WaitForLeader(t, s1.RPC)

	s2, cleanupS2 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 0
		c.RaftConfig.ProtocolVersion = 3
		c.Build = "0.8.0"
	})
	defer cleanupS2()

	s3, cleanupS3 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 0
		c.RaftConfig.ProtocolVersion = 3
		c.Build = "0.8.1"
	})
	defer cleanupS3()

	TestJoin(t, s1, s2, s3)

	// Wait for both servers to be added as voters
	retry.Run(t, func(r *retry.R) {
		future := s1.raft.GetConfiguration()
		if err := future.Error(); err != nil {
			r.Fatal(err)
		}

		for _, s := range future.Configuration().Servers {
			if got, want := s.Suffrage, raft.Voter; got != want {
				r.Fatalf("got %v want %v", got, want)
			}
		}

		if !s1.IsLeader() {
			r.Fatal("server 1 should be leader")
		}
	})
}

func findServer(t *testing.T, servers []raft.Server, s *Server) raft.Server {
	t.Helper()

	id := s.config.RaftConfig.LocalID
	for _, rs := range servers {
		if rs.ID == id {
			return rs
		}
	}

	t.Fatalf("server %v not found: %v", id, servers)
	panic("cannot happen")
}
