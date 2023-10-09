//go:build ent
// +build ent

package reporting

import (
	"bytes"
	"crypto/md5"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/go-census"
	"github.com/hashicorp/go-census/exporter"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/licensingexporter"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/nomad/license"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/nomad/structs/config"
	"github.com/hashicorp/nomad/version"
	"github.com/oklog/ulid"
)

const (
	reportingInterval            = 1 * time.Hour
	exportingInterval            = 24 * time.Hour
	exportingIntervalDescription = "daily license reporting export"
)

type ClientsCounter interface {
	GetClientNodesCount() (int, error)
}

type Manager struct {
	enabled        bool
	logger         hclog.Logger
	census         *census.Agent
	clientsCounter ClientsCounter

	license *nomadLicense.License
}

func NewManager(logger hclog.Logger, config *config.ReportingConfig,
	lic *nomadLicense.License, cg ClientsCounter) (*Manager, error) {
	m := &Manager{
		logger:         logger.Named("reporting"),
		license:        lic,
		clientsCounter: cg,
	}

	if config.License != nil && config.License.Enabled != nil && *config.License.Enabled {
		m.logger.Info("reporting agent is enabled and configured")
		m.enabled = true
		return m, nil
	}

	return m, nil
}

func (rm *Manager) Start(stopCh chan struct{}, meta structs.ClusterMetadata) error {
	if !rm.enabled {
		rm.logger.Info("license reporting disabled")
		return nil
	}

	if meta.ClusterID == "" {
		return errors.New("missing cluster ID")
	}

	err := rm.startCensusAgent(meta)
	if err != nil {
		return fmt.Errorf("could not start go census agent: %w", err)
	}

	rm.logger.Info("reporting agent started")

	go rm.run(stopCh)

	return nil
}

func (rm *Manager) startCensusAgent(meta structs.ClusterMetadata) error {
	licExporter, err := setupLicenseExporter(rm.logger, rm.license)
	if err != nil {
		return err
	}

	caAgent, err := setupReportingAgent(rm.logger, licExporter, meta)
	if err != nil {
		return fmt.Errorf("could not instantiate reporting agent: %w", err)
	}

	rm.census = caAgent
	return nil
}

func setupLicenseExporter(log hclog.Logger, lic *nomadLicense.License) (*licensingexporter.Exporter, error) {
	exporterCfg := licensingexporter.Config{
		License:        *lic.License,
		Product:        "nomad",
		ProductVersion: version.GetVersion().Version,
		Logger:         log.Named("license-exporter"),
		AuditConfig: licensingexporter.AuditConfig{
			UseDefaultLogger: true,
		},
	}

	// create the license exporter
	return licensingexporter.New(exporterCfg)
}

func setupReportingAgent(log hclog.Logger, exp exporter.Exporter, meta structs.ClusterMetadata) (*census.Agent, error) {
	id, err := generateULIDFromMetadata(meta)
	if err != nil {
		return nil, fmt.Errorf("unable to convert process ID: %w", err)
	}

	// create reporting configuration
	reportingCfg := census.Config{
		ProcessIDULID: id,
		Schema:        license.NewCensusSchema(),
		Exporters: map[string]exporter.Config{
			"licensing": {
				Schedule: exporter.Schedule{
					Name:     exportingIntervalDescription,
					Interval: exportingInterval,
				},
				Exporter: exp,
				Enabled:  true,
			},
		},
	}

	return census.New(reportingCfg, census.WithLogger(log.Named("agent")))
}

func (rm *Manager) stop() error {

	err := rm.census.Shutdown()
	if err != nil {
		rm.logger.Error("failed to shutdown reporting agent", "error", err)
		return err
	}

	rm.logger.Info("reporting agent was shut down")

	return nil
}

func (rm *Manager) run(stopCh chan struct{}) {
	rm.logger.Debug("starting reporting background routine")

	ticker := time.NewTicker(reportingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			rm.logger.Debug("reporting background routine shutting down")
			err := rm.stop()
			if err != nil {
				rm.logger.Error("failed stop the reporting manager", "error", err)
			}

			return
		case <-ticker.C:
			err := rm.writeMetrics()
			if err != nil {
				rm.logger.Warn("failed to write metrics for reporting store", "error", err)
			}
		}
	}
}

func (rm *Manager) writeMetrics() error {
	ns, err := rm.clientsCounter.GetClientNodesCount()
	if err != nil {
		rm.logger.Debug("unable to get client nodes count", err.Error())
		return err
	}

	return rm.census.Write(license.BillableClientsMetricKey, float64(ns))
}

func generateULIDFromMetadata(meta structs.ClusterMetadata) (string, error) {
	// Convert the timestamp to milliseconds since Unix epoch
	ulidTimestamp := uint64(meta.CreateTime / 1000000)
	// Generate randomness based on the MD5 hash of the UUID
	hash := md5.Sum([]byte(meta.ClusterID))

	ulID, err := ulid.New(ulidTimestamp, bytes.NewReader(hash[:10]))
	if err != nil {
		return "", fmt.Errorf("unable to build process ID: %w", err)
	}

	return ulID.String(), nil
}
