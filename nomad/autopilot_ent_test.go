//go:build ent
// +build ent

package nomad

import (
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/raft"
	"github.com/shoenig/test/must"

	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/testutil"
)

func TestAutopilotEnterprise_DesignateNonVoter(t *testing.T) {
	ci.Parallel(t)

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 2
		c.RaftConfig.ProtocolVersion = 3
		c.NumSchedulers = 0 // reduce log noise
	})
	defer cleanupS1()

	s2, cleanupS2 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 2
		c.RaftConfig.ProtocolVersion = 3
		c.NumSchedulers = 0 // reduce log noise
	})
	defer cleanupS2()

	s3, cleanupS3 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 2
		c.NonVoter = true
		c.RaftConfig.ProtocolVersion = 3
		c.NumSchedulers = 0 // reduce log noise
	})
	defer cleanupS3()

	TestJoin(t, s1, s2, s3)

	testutil.WaitForLeader(t, s1.RPC)

	// wait twice the server stabilization threshold to give the server
	// time to be promoted
	time.Sleep(2 * s1.config.AutopilotConfig.ServerStabilizationTime)

	testutil.WaitForResultUntil(10*time.Second, func() (bool, error) {
		future := s1.raft.GetConfiguration()
		if err := future.Error(); err != nil {
			return false, err
		}
		servers := future.Configuration().Servers
		if len(servers) != 3 {
			return false, fmt.Errorf("expected 3 servers, got: %v", servers)
		}
		// s2 should be a voter
		if !isPotentialVoter(findServer(t, servers, s2).Suffrage) {
			return false, fmt.Errorf("expected s2 to be voter: %v", servers)
		}

		// s3 should remain a non-voter
		if isPotentialVoter(findServer(t, servers, s3).Suffrage) {
			return false, fmt.Errorf("expected s3 to be non-voter: %v", servers)
		}
		return true, nil
	}, func(err error) { must.NoError(t, err) })
}

func isPotentialVoter(suffrage raft.ServerSuffrage) bool {
	switch suffrage {
	case raft.Voter, raft.Staging:
		return true
	default:
		return false
	}
}

func TestAutopilotEnterprise_RedundancyZone(t *testing.T) {
	ci.Parallel(t)

	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 3
		c.RaftConfig.ProtocolVersion = 3
		c.AutopilotConfig.EnableRedundancyZones = true
		c.RedundancyZone = "east"
		c.NumSchedulers = 0 // reduce log noise
	})
	defer cleanupS1()

	s2, cleanupS2 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 3
		c.RaftConfig.ProtocolVersion = 3
		c.AutopilotConfig.EnableRedundancyZones = true
		c.RedundancyZone = "west"
		c.NumSchedulers = 0 // reduce log noise
	})
	defer cleanupS2()

	s3, cleanupS3 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 3
		c.RaftConfig.ProtocolVersion = 3
		c.AutopilotConfig.EnableRedundancyZones = true
		c.RedundancyZone = "west-2"
		c.NumSchedulers = 0 // reduce log noise
	})
	defer cleanupS3()

	TestJoin(t, s1, s2, s3)

	t.Logf("waiting for initial stable cluster")
	waitForStableLeadership(t, []*Server{s1, s2, s3})

	t.Logf("adding server s4")
	s4, cleanupS4 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 0
		c.RaftConfig.ProtocolVersion = 3
		c.AutopilotConfig.EnableRedundancyZones = true
		c.RedundancyZone = "west-2"
		c.NumSchedulers = 0 // reduce log noise
	})
	defer cleanupS4()

	TestJoin(t, s1, s4)
	t.Log("waiting to ensure s4 is not promoted to voter")
	time.Sleep(2*s1.config.AutopilotConfig.ServerStabilizationTime + s1.config.AutopilotInterval)
	{
		future := s1.raft.GetConfiguration()
		must.NoError(t, future.Error())
		servers := future.Configuration().Servers
		must.Eq(t, findServer(t, servers, s4).Suffrage, raft.Nonvoter)
	}

	t.Logf("shutting down server s3")
	s3.Shutdown()

	t.Logf("waiting for cluster to stabilize and s4 to become a voter")
	waitForStableLeadership(t, []*Server{s1, s2, s4})

}

func TestAutopilotEnterprise_UpgradeMigration(t *testing.T) {
	ci.Parallel(t)
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 2
		c.RaftConfig.ProtocolVersion = 3
		c.Build = "0.8.0"
		c.NumSchedulers = 0 // reduce log noise
	})
	defer cleanupS1()

	s2, cleanupS2 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 2
		c.RaftConfig.ProtocolVersion = 3
		c.Build = "0.8.1"
		c.NumSchedulers = 0 // reduce log noise
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

func TestAutopilotEnterprise_CustomUpgradeMigration(t *testing.T) {
	ci.Parallel(t)
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 2
		c.RaftConfig.ProtocolVersion = 3
		c.AutopilotConfig.EnableCustomUpgrades = true
		c.UpgradeVersion = "0.0.1"
		c.NumSchedulers = 0 // reduce log noise
	})
	defer cleanupS1()

	s2, cleanupS2 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 2
		c.RaftConfig.ProtocolVersion = 3
		c.AutopilotConfig.EnableCustomUpgrades = true
		c.UpgradeVersion = "0.0.2"
		c.NumSchedulers = 0 // reduce log noise
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

func TestAutopilotEnterprise_DisableUpgradeMigration(t *testing.T) {
	ci.Parallel(t)
	s1, cleanupS1 := TestServer(t, func(c *Config) {
		c.RaftConfig.ProtocolVersion = 3
		c.AutopilotConfig.DisableUpgradeMigration = true
		c.Build = "0.8.0"
		c.NumSchedulers = 0 // reduce log noise
	})
	defer cleanupS1()

	testutil.WaitForLeader(t, s1.RPC)

	s2, cleanupS2 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 0
		c.RaftConfig.ProtocolVersion = 3
		c.Build = "0.8.0"
		c.NumSchedulers = 0 // reduce log noise
	})
	defer cleanupS2()

	s3, cleanupS3 := TestServer(t, func(c *Config) {
		c.BootstrapExpect = 0
		c.RaftConfig.ProtocolVersion = 3
		c.Build = "0.8.1"
		c.NumSchedulers = 0 // reduce log noise
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
