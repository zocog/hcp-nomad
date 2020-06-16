package snapshotagent

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-uuid"
	snapshot "github.com/hashicorp/raft-snapshot"
	"github.com/hashicorp/raft-snapshotagent/internal/logging"
)

const (
	// agentTTL controls the TTL of the "agent alive" check, and also
	// determines how often we poll the agent to check on service
	// registration.
	agentTTL = 30 * time.Second

	// retryTime is used as a dead time before retrying a failed operation,
	// and is also used when trying to give up leadership to another
	// instance.
	retryTime = 10 * time.Second
)

type Snapshotter func(ctx context.Context, allowStale bool) (io.ReadCloser, error)

// Agent is a snapshot agent that can run as a daemon or in a one-shot mode for
// batch jobs.
type Agent struct {
	// config is the configuration of the agent as given at create time.
	config *Config

	// logger is used to present information once the agent is running.
	logger hclog.Logger

	// consul is used to interact with Consul to take snapshots.
	consul *api.Client

	snapshotter Snapshotter

	// librarian manages storage of snapshots.
	librarian Librarian

	// id is a UUID for this instance of the snapshot agent, which is used
	// for service registration to disambiguate different instances on the
	// same host.
	id string
}

// NewAgent returns a snapshot agent with the given configuration and
// dependencies.
func New(product string, snapshotter Snapshotter, config *Config, logger hclog.Logger) (*Agent, error) {
	config = config.setDefaults(product)
	err := config.setDerivedFields()
	if err != nil {
		return nil, err
	}

	consulClientConf := config.Consul.toConsulClientConfig(logger)

	var consul *api.Client
	if consulClientConf != nil {
		var err error
		consul, err = api.NewClient(consulClientConf)
		if err != nil {
			return nil, fmt.Errorf("failed to create consul client: %v", err)
		}
	}

	librarian, err := config.toLibrarian(product, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create librarian: %v", err)
	}

	return NewAgent(config, snapshotter, consul, librarian, logger)

}

func NewAgent(config *Config, snapshotter Snapshotter, consul *api.Client, librarian Librarian, logger hclog.Logger) (*Agent, error) {
	if err := config.setDerivedFields(); err != nil {
		return nil, err
	}

	agent := &Agent{
		config:      config,
		logger:      logger.Named(logging.Snapshot),
		consul:      consul,
		librarian:   librarian,
		snapshotter: snapshotter,
	}

	// Generate a unique ID for this agent so we can disambiguate different
	// instances on the same host.
	id, err := uuid.GenerateUUID()
	if err != nil {
		return nil, err
	}

	agent.id = id
	return agent, nil
}

// register is used to register this agent with Consul service discovery.
func (a *Agent) register(serviceID string) error {
	if a.consul == nil {
		return nil
	}

	service := &api.AgentServiceRegistration{
		ID:   serviceID,
		Name: a.config.Agent.Service,
	}
	if err := a.consul.Agent().ServiceRegister(service); err != nil {
		return err
	}

	return nil
}

func (a *Agent) deregister(serviceID string) error {
	if a.consul == nil {
		return nil
	}

	return a.consul.Agent().ServiceDeregister(serviceID)
}

// Run is a long-lived routine that will take snapshots at the given
// interval until the shutdownCh is closed. This will block until that
// happens. It will perform leader election and register this agent with
// Consul's service discovery, with some associated health checks. If the
// interval is == 0 this will run once and exit without leader election or
// service registration.
func (a *Agent) Run(ctx context.Context) error {
	// See if we can shortcut to one-shot mode.
	if a.config.Agent.interval == 0 {
		return a.snapshotAndRotate()
	}

	if a.consul == nil {
		a.logger.Info("consul is not configured; skipping service registration and locking")
	}

	// Do the initial service registration.
	serviceID := fmt.Sprintf("%s:%s", a.config.Agent.Service, a.id)
	if err := a.register(serviceID); err != nil {
		return err
	}

	// Run the agent's work routines.
	var wg sync.WaitGroup
	wrapAndGo := func(f func(context.Context, string)) {
		wg.Add(1)
		go func() {
			f(ctx, serviceID)
			wg.Done()
		}()
	}
	wrapAndGo(a.runRegister)
	wrapAndGo(a.runTTL)
	wrapAndGo(a.runSnapshots)

	// Wait for shutdown.
	<-ctx.Done()
	wg.Wait()

	// Clean up.
	if err := a.deregister(serviceID); err != nil {
		return err
	}

	return nil
}

// runRegister is a long-running goroutine that ensures this agent is registered
// with Consul's service discovery. It will run until the shutdownCh is closed.
func (a *Agent) runRegister(ctx context.Context, serviceID string) {
	if a.consul == nil {
		return
	}

	for {
	REGISTER_CHECK:
		select {
		case <-ctx.Done():
			return

		case <-time.After(agentTTL):
			services, err := a.consul.Agent().Services()
			if err != nil {
				a.logger.Error("Failed to check services (will retry)", "error", err)
				time.Sleep(retryTime)
				goto REGISTER_CHECK
			}

			if _, ok := services[serviceID]; !ok {
				a.logger.Warn("Service registration was lost, reregistering")
				if err := a.register(serviceID); err != nil {
					a.logger.Error("Failed to reregister service (will retry)", "error", err)
					time.Sleep(retryTime)
					goto REGISTER_CHECK
				}
			}
		}
	}
}

// runTTL is a long-running goroutine that registers an "agent alive" TTL health
// check and services it periodically. It will run until the shutdownCh is closed.
func (a *Agent) runTTL(ctx context.Context, serviceID string) {
	if a.consul == nil {
		return
	}

	ttlID := fmt.Sprintf("%s:agent-ttl", serviceID)

REGISTER:
	select {
	case <-ctx.Done():
		return
	default:
	}

	// Perform an initial registration with retries.
	check := &api.AgentCheckRegistration{
		ID:        ttlID,
		Name:      "Consul Snapshot Agent Alive",
		Notes:     "This check is periodically updated as long as the agent is alive.",
		ServiceID: serviceID,
		AgentServiceCheck: api.AgentServiceCheck{
			TTL:                            agentTTL.String(),
			Status:                         api.HealthPassing,
			DeregisterCriticalServiceAfter: a.config.Agent.deregisterAfter.String(),
		},
	}
	if err := a.consul.Agent().CheckRegister(check); err != nil {
		a.logger.Error("Failed to register TTL check (will retry)", "error", err)
		time.Sleep(retryTime)
		goto REGISTER
	}

	// Wait for the shutdown signal and poke the TTL check.
	for {
		select {
		case <-ctx.Done():
			return

		case <-time.After(agentTTL / 2):
			if err := a.consul.Agent().UpdateTTL(ttlID, "", api.HealthPassing); err != nil {
				a.logger.Error("Failed to refresh agent TTL check (will reregister)", "error", err)
				time.Sleep(retryTime)
				goto REGISTER
			}
		}
	}
}

// runSnapshots is a long-running goroutine that will perform a leader election,
// register a "snapshots healthy" TTL check and periodically service it, and
// perform the actual snapshots. It will run until the shutdownCh is closed.
func (a *Agent) runSnapshots(ctx context.Context, serviceID string) {
	// Arrange to give up any held lock any time we exit the goroutine so
	// another agent can pick up without delay.
	var lock *api.Lock
	defer func() {
		if lock != nil {
			lock.Unlock()
		}
	}()

	ttlID := fmt.Sprintf("%s:snapshot-ttl", serviceID)
	var leaderCh <-chan struct{}
	var err error
LEADER_WAIT:
	select {
	case <-ctx.Done():
		return
	default:
	}

	if a.consul == nil {
		goto RUN_SNAPSHOT
	}

	// If there's an existing TTL check, set it to critical immediately
	// since we lost leadership. If any of this fails we move on, since
	// it's a TTL check, so it should fail on its own.
	if a.hasCheck(ttlID) {
		if err := a.consul.Agent().UpdateTTL(ttlID, "", api.HealthCritical); err != nil {
			a.logger.Error("Failed to set agent TTL check to critical", "error", err)
		}
	}

	// Wait to get the leader lock before running snapshots.
	a.logger.Info("Waiting to obtain leadership...")
	if lock == nil {
		var err error
		lock, err = a.consul.LockKey(a.config.Agent.LockKey)
		if err != nil {
			a.logger.Error("Error trying to create leader lock (will retry)", "error", err)
			time.Sleep(retryTime)
			goto LEADER_WAIT
		}
	}

	leaderCh, err = lock.Lock(ctx.Done())
	if err != nil {
		if err == api.ErrLockHeld {
			a.logger.Error("Unable to use leader lock that was held previously and presumed lost, giving up the lock (will retry)", "error", err)
			lock.Unlock()
			time.Sleep(retryTime)
			goto LEADER_WAIT
		} else {
			a.logger.Error("Error trying to get leader lock (will retry)", "error", err)
			time.Sleep(retryTime)
			goto LEADER_WAIT
		}
	}
	if leaderCh == nil {
		// This is how the Lock() call lets us know that it quit because
		// we closed the shutdown channel.
		return
	}
	a.logger.Info("Obtained leadership")

	// Install a leader check that is healthy as long as we are taking
	// snapshots (we set this to 2X the interval). This also covers us if
	// this goroutine dies for some reason, so it's an active check. If this
	// fails and we lose leadership, it will be marked critical immediately
	// and remain critical until this agent becomes leader again and tries
	// taking snapshots.
	if !a.hasCheck(ttlID) {
		check := &api.AgentCheckRegistration{
			ID:        ttlID,
			Name:      "Consul Snapshot Agent Saving Snapshots",
			Notes:     "This check is periodically updated as long as the leader is successfully taking snapshots.",
			ServiceID: serviceID,
			AgentServiceCheck: api.AgentServiceCheck{
				TTL:    (2 * a.config.Agent.interval).String(),
				Status: api.HealthPassing,
			},
		}
		if err := a.consul.Agent().CheckRegister(check); err != nil {
			a.logger.Error("Failed to register TTL check (will give up leadership)", "error", err)
			lock.Unlock()
			time.Sleep(retryTime)
			goto LEADER_WAIT
		}
	}

RUN_SNAPSHOT:

	// We use this to decide when to give up, even if we have the leader
	// lock.
	var failures int
	for {
		// Try to take a snapshot.
		a.logger.Debug("Taking a snapshot...")
		if err := a.snapshotAndRotate(); err != nil {
			a.logger.Error("Snapshot failed (will retry at next interval)", "error", err)
			failures++
		} else if a.consul != nil {
			if err := a.consul.Agent().UpdateTTL(ttlID, "", api.HealthPassing); err != nil {
				a.logger.Error("Failed to refresh agent TTL check", "error", err)
				failures++
			} else {
				failures = 0
			}
		}

		// See if we have failed too many times.
		if a.consul != nil && (a.config.Agent.MaxFailures == nil || failures > *a.config.Agent.MaxFailures) {
			a.logger.Warn("Too many snapshot failures (will give up leadership)")
			lock.Unlock()
			time.Sleep(retryTime)
			goto LEADER_WAIT
		}

		// Wait for the next event.
		select {
		case <-time.After(a.config.Agent.interval):
			// Loop and attempt a snapshot.
		case <-leaderCh:
			a.logger.Warn("Lost leadership, stopping snapshots")
			goto LEADER_WAIT
		case <-ctx.Done():
			return
		}
	}
}

// snapshotAndRotate performs a single cycle of taking a snapshot and rotating
// it in to the library, and possibly deleting some old snapshots.
func (a *Agent) snapshotAndRotate() error {
	snap, err := a.snapshotter(context.Background(), a.config.Agent.AllowStale)
	if err != nil {
		return fmt.Errorf("failed to snapshot: %v", err)
	}
	defer snap.Close()

	// Stream this snapshot to disk in the scratch space first.
	tmpFile, err := ioutil.TempFile(a.config.Local.Path, "unverified-snapshot")
	if err != nil {
		return fmt.Errorf("could not open local scratch file for saving the unverified snapshot: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := io.Copy(tmpFile, snap); err != nil {
		return fmt.Errorf("could not write local unverified snapshot: %v", err)
	}

	// Rewind file for verification.
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := snapshot.Verify(tmpFile); err != nil {
		return fmt.Errorf("snapshot verification failed: %v", err)
	}

	// Rewind file again, now to stream to the librarian.
	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		return err
	}

	id := SnapshotID(time.Now().UTC().UnixNano())
	if err := a.librarian.Create(id, tmpFile); err != nil {
		return err
	}
	if a.librarian.RotationEnabled() {
		a.logger.Info("Saved snapshot", "id", id)
	} else {
		a.logger.Info("Saved snapshot")
	}

	if retain := *a.config.Agent.Retain; retain > 0 && a.librarian.RotationEnabled() {
		n, err := Rotate(a.librarian, retain, 2*retain)
		if err != nil {
			return err
		}
		a.logger.Debug("Rotated snapshots", "number_deleted", n)
	}

	return nil
}

// hasCheck is a helper that queries the Consul agent and checks to see if it
// has the given check registered.
func (a *Agent) hasCheck(checkID string) bool {
	checks, err := a.consul.Agent().Checks()
	if err != nil {
		a.logger.Error("Error trying to find existing check", "error", err)
		return false
	}

	_, ok := checks[checkID]
	return ok
}

func (a *Agent) Info() string {
	var b strings.Builder
	fmt.Fprintf(&b, "        Interval: %q\n", a.config.Agent.interval)
	fmt.Fprintf(&b, "          Retain: %d\n", a.config.Agent.Retain)
	fmt.Fprintf(&b, "           Stale: %v\n", a.config.Agent.AllowStale)

	localScratchPath := os.TempDir()
	if a.config.Agent.LocalScratchPath != "" {
		localScratchPath = a.config.Agent.LocalScratchPath
	}
	fmt.Fprintf(&b, "   Local Scratch: %s\n", localScratchPath)

	if a.config.Agent.interval > 0 {
		fmt.Fprintf(&b, "            Mode: daemon\n")
		fmt.Fprintf(&b, "         Service: %q\n", a.config.Agent.Service)
		fmt.Fprintf(&b, "Deregister After: %q\n", a.config.Agent.deregisterAfter)
		fmt.Fprintf(&b, "        Lock Key: %q\n", a.config.Agent.LockKey)
		fmt.Fprintf(&b, "    Max Failures: %d\n", a.config.Agent.MaxFailures)
	} else {
		fmt.Fprintf(&b, "            Mode: one-shot\n")
	}
	fmt.Fprintf(&b, "Snapshot Storage: %s", a.librarian.Info())

	return b.String()
}
