// +build ent

package nomad

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/go-licensing"
	"github.com/hashicorp/go-memdb"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/sentinel/sentinel"
)

// Temporary Nomad-Enterprise licenses should operate for six hours
var temporaryLicenseTimeLimit = time.Hour * 6

type EnterpriseState struct {
	// sentinel is a shared instance of the policy engine
	sentinel *sentinel.Sentinel
}

// setupEnterprise is used for Enterprise specific setup
func (s *Server) setupEnterprise(config *Config) error {
	// Enable the standard lib, except the HTTP import.
	stdlib := sentinel.ImportsMap(sentinel.StdImports())
	stdlib.Blacklist([]string{"http"})

	// Setup the sentinel configuration
	sentConf := &sentinel.Config{
		Imports: stdlib,
	}
	if config.SentinelConfig != nil {
		for _, sImport := range config.SentinelConfig.Imports {
			sentConf.Imports[sImport.Name] = &sentinel.Import{
				Path: sImport.Path,
				Args: sImport.Args,
			}
			s.logger.Named("sentinel").Debug("enabling imports", "name", sImport.Name, "path", sImport.Path)
		}
	}

	// Create the Sentinel instance based on the configuration
	s.sentinel = sentinel.New(sentConf)

	s.setupEnterpriseAutopilot(config)

	if err := s.setupWatcher(s.shutdownCtx); err != nil {
		s.logger.Named("licensing").Error("failed to setup license watcher", "error", err)
		s.Shutdown()
	}

	return nil
}

func watcherStartupOpts() (*licensing.WatcherOptions, error) {
	tempLicense, pubKey, err := licensing.TemporaryLicense(nomadLicense.ProductName, nil, temporaryLicenseTimeLimit)
	if err != nil {
		return nil, fmt.Errorf("failed creating temporary license: %w", err)
	}
	return &licensing.WatcherOptions{
		ProductName:          nomadLicense.ProductName,
		InitLicense:          tempLicense,
		AdditionalPublicKeys: []string{pubKey},
	}, nil
}

func (s *Server) setupWatcher(ctx context.Context) error {
	// setup a logger for licensing
	licLogger := s.logger.Named("licensing")

	// Configure the setup options for the license watcher
	opts, err := watcherStartupOpts()
	if err != nil {
		return fmt.Errorf("failed setting up license watcher options: %w", err)
	}

	// Create the new watcher with options
	watcher, _, err := licensing.NewWatcher(opts)
	if err != nil {
		return fmt.Errorf("failed creating license watcher: %w", err)
	}
	s.Watcher = watcher

	go func() {
		// licenseSet tracks whether or not a permanent license has been set
		var licenseSet bool

		// watchSetEmitted tracks whether or not the watch set has emitted an error
		var watchSetEmitted bool

		// watchSetCh is used to cache the watch set chan for this goroutine
		var watchSetCh <-chan error
		watchSet := memdb.NewWatchSet()

		for {
			stored, err := s.State().License(watchSet)
			if err != nil {
				licLogger.Error("failed fetching license from state store", "error", err)
			}
			if stored != nil && stored.Signed != "" {
				if _, err := watcher.SetLicense(stored.Signed); err != nil {
					licLogger.Error("failed setting license", "error", err)
				}
				licenseSet = true
			}

			// Create a new watchSetCh if it's our first or the previous has emitted
			if watchSetCh == nil || watchSetEmitted {
				watchSetCh = watchSet.WatchCh(s.shutdownCtx)
				watchSetEmitted = false
			}

			select {
			// Exit the goroutine if the server is closing
			case <-s.shutdownCh:
				return
			// If watchSetCh returns a nil error, there is a new license. If it returns an actual error,
			// the context is done or canceled, and the function can exit.
			case err := <-watchSetCh:
				watchSetEmitted = true
				if err != nil {
					licLogger.Debug("retreiving new license")
				} else {
					return
				}
			// This final case checks for licensing errors, primarily expirations.
			case err := <-s.Watcher.ErrorCh():
				licLogger.Error("received error from watcher", "error", err)

				// If a permanent license has not been set, we close the server.
				if !licenseSet {
					licLogger.Error("temporary license expired; shutting down server")
					s.Shutdown()
					return
				}
				licLogger.Error("license expired") //TODO: more info
			case warnLicense := <-s.Watcher.WarningCh():
				licLogger.Warn("license expiring", "time_left", time.Until(warnLicense.ExpirationTime).Truncate(time.Second))
			}
		}
	}()
	return nil
}

// startEnterpriseBackground starts the Enterprise specific workers
func (s *Server) startEnterpriseBackground() {
	// Garbage collect Sentinel policies if enabled
	if s.config.ACLEnabled {
		go s.gcSentinelPolicies(s.shutdownCh)
	}
}
