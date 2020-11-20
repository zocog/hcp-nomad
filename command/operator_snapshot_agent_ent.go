package command

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/nomad/api"
	"github.com/hashicorp/nomad/command/agent"
	"github.com/hashicorp/nomad/helper/logging"
	snapshotagent "github.com/hashicorp/raft-snapshotagent"
	"github.com/posener/complete"
)

type OperatorSnapshotAgentCommand struct {
	Meta
}

func (c *OperatorSnapshotAgentCommand) Help() string {
	helpText := `
Usage: nomad operator snapshot agent [options] <config_file>

  The snapshot agent takes snapshots of the state of the nomad servers and
  saves them locally, or pushes them to an optional remote storage service.

  The agent can be run as a long-running daemon process or in a one-shot mode
  from a batch job. As a long-running daemon, the agent will perform a leader
  election so multiple processes can be run in a highly available fashion with
  automatic failover. In daemon mode, the agent will also register itself with
  Nomad as a service, along with health checks that show the agent is alive
  and able to take snapshots.

  If ACLs are enabled, a management token must be supplied in order to perform
  snapshot operations.

  For a full list of options and examples, please see the nomad documentation.

  The Config file has the following format (shown populated with default values):

nomad {
  address         = "http://127.0.0.1:4646"
  token           = ""
  region          = ""
  ca_file         = ""
  ca_path         = ""
  cert_file       = ""
  key_file        = ""
  tls_server_name = ""
}

` + snapshotagent.SampleConfig("nomad") + `

` + snapshotagent.FlagsHelp("nomad") + `
General Options:
	` + generalOptionsUsage(usageOptsDefault)

	return strings.TrimSpace(helpText)
}

func (c *OperatorSnapshotAgentCommand) AutocompleteFlags() complete.Flags {
	return complete.Flags{}
}

func (c *OperatorSnapshotAgentCommand) AutocompleteArgs() complete.Predictor {
	return mergeAutocompleteFlags(c.Meta.AutocompleteFlags(FlagSetClient),
		complete.Flags{})
}

func (c *OperatorSnapshotAgentCommand) Synopsis() string {
	return "Periodically saves snapshots of Nomad server state"
}

func (c *OperatorSnapshotAgentCommand) Name() string { return "operator snapshot agent" }

func (c *OperatorSnapshotAgentCommand) Run(args []string) int {
	var configFile string
	var config NomadSnapshotAgentConfig

	// Check that we either got no filename or exactly one.
	flags := c.Meta.FlagSet(c.Name(), FlagSetClient)
	flags.Usage = func() { c.Ui.Output(c.Help()) }

	flags.StringVar(&configFile, "config-file", "", "")
	config.InstallFlags(flags)

	if err := flags.Parse(args); err != nil {
		c.Ui.Error(fmt.Sprintf("Failed to parse args: %v", err))
		return 1
	}

	args = flags.Args()
	if len(args) > 1 {
		c.Ui.Error("This command takes a single argument: <filename>")
		c.Ui.Error(commandErrorText(c))
		return 1
	}

	if len(args) == 1 {
		path := args[0]
		f, err := os.Open(path)
		if err != nil {
			c.Ui.Error(fmt.Sprintf("Error opening snapshot config file: %s", err))
			return 1
		}

		err = snapshotagent.DecodeFile(path, f, &config)
		f.Close()
		if err != nil {
			c.Ui.Error(fmt.Sprintf("Error parsing snapshot config file: %s", err))
			return 1
		}
	}

	if config.Nomad == nil {
		config.Nomad = &NomadConfig{}
	}
	setDefault(&config.Nomad.HTTPAddr, c.Meta.flagAddress)
	c.Meta.flagAddress = config.Nomad.HTTPAddr
	if config.Nomad.Address != "" {
		c.Meta.flagAddress = config.Nomad.Address
	}
	setDefault(&config.Nomad.Token, c.Meta.token)
	c.Meta.token = config.Nomad.Token
	setDefault(&config.Nomad.Region, c.Meta.region)
	c.Meta.region = config.Nomad.Region
	setDefault(&config.Nomad.CAFile, c.Meta.caCert)
	c.Meta.caCert = config.Nomad.CAFile
	setDefault(&config.Nomad.CAPath, c.Meta.caPath)
	c.Meta.caPath = config.Nomad.CAPath
	setDefault(&config.Nomad.CertFile, c.Meta.clientCert)
	c.Meta.clientCert = config.Nomad.CertFile
	setDefault(&config.Nomad.KeyFile, c.Meta.clientKey)
	c.Meta.clientKey = config.Nomad.KeyFile
	setDefault(&config.Nomad.TLSServerName, c.Meta.tlsServerName)
	c.Meta.tlsServerName = config.Nomad.TLSServerName

	_, err := config.Canonicalize("nomad")
	if err != nil {
		c.Ui.Error(fmt.Sprintf("Error validating Nomad config: %v", err))
		return 1
	}
	client, err := c.Meta.Client()
	if err != nil {
		c.Ui.Error(fmt.Sprintf("Error creating a Nomad API: %v", err))
		return 1
	}

	snapshotter := func(_ context.Context, allowStale bool) (io.ReadCloser, error) {
		q := &api.QueryOptions{}
		q.AllowStale = allowStale

		return client.Operator().Snapshot(q)
	}

	logConfig := &agent.Config{
		LogLevel:       "INFO",
		LogJson:        false,
		EnableSyslog:   false,
		SyslogFacility: "SUS",
	}
	if l := config.Config.Log; l != nil {
		if l.Level != "" {
			logConfig.LogLevel = l.Level
		}
		logConfig.LogJson = l.LogJSON
		logConfig.EnableSyslog = l.EnableSyslog
		if l.SyslogFacility != "" {
			logConfig.SyslogFacility = l.SyslogFacility
		}
	}

	_, logGate, logOutput := agent.SetupLoggers(c.Ui, logConfig)
	// Create logger
	logger := hclog.NewInterceptLogger(&hclog.LoggerOptions{
		Name:       "snapshotagent",
		Level:      hclog.LevelFromString(logConfig.LogLevel),
		Output:     logOutput,
		JSONFormat: logConfig.LogJson,
	})
	// Swap out UI implementation if json logging is enabled
	if logConfig.LogJson {
		c.Ui = &logging.HcLogUI{Log: logger}
	}

	sagent, err := snapshotagent.New("nomad", snapshotter, &config.Config, logger)
	if err != nil {
		c.Ui.Error(fmt.Sprintf("Error create agent: %v", err))
		return 1
	}

	c.Ui.Output("Nomad snapshot agent running!")
	c.Ui.Output(sagent.Info())
	c.Ui.Info("")
	c.Ui.Output("Log data will now stream in as it occurs:\n")
	logGate.Flush()

	// Wait for the shutdown signal, then notify the snapshotter and wait
	// for it to clean up.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalCh
		logger.Info("Shutdown triggered, waiting for agent to clean up...")
		cancel()
	}()

	if err := checkLicense(ctx, client, logger); err != nil {
		if ctx.Err() != nil {
			logger.Error("failed to check license", "error", err)
		}
		return 1
	}

	// Run the agent!
	if err := sagent.Run(ctx); err != nil {
		logger.Error("Snapshot agent error", "error", err)
		return 1
	}

	return 0
}

type NomadSnapshotAgentConfig struct {
	Nomad                *NomadConfig `hcl:"nomad,block"`
	snapshotagent.Config `hcl:",remain"`
}

type NomadConfig struct {
	Address       string `hcl:"address,optional"`
	HTTPAddr      string `hcl:"http_addr,optional"` // deprecated in favor of address
	Token         string `hcl:"token,optional"`
	Region        string `hcl:"region,optional"`
	CAFile        string `hcl:"ca_file,optional"`
	CAPath        string `hcl:"ca_path,optional"`
	CertFile      string `hcl:"cert_file,optional"`
	KeyFile       string `hcl:"key_file,optional"`
	TLSServerName string `hcl:"tls_server_name,optional"`
}

func setDefault(v *string, def string) {
	if def != "" {
		*v = def
	}
}

func checkLicense(ctx context.Context, client *api.Client, logger hclog.Logger) error {
	logger = logger.Named("license")
	autoBackupFeature := license.FeatureAutoBackups.String()

	checkLisense := func(index uint64) (bool, uint64, error) {
		q := &api.QueryOptions{WaitIndex: index}
		if q.WaitIndex == 0 {
			q.WaitIndex = 1
		}
		li, qm, err := client.Operator().LicenseGet(q)
		if err != nil {
			return false, 0, err
		}

		for _, f := range li.License.Features {
			if f == autoBackupFeature {
				return true, qm.LastIndex, nil
			}
		}
		return false, qm.LastIndex, nil
	}

	licensedCh := make(chan struct{})

	go func() {
		timer := time.NewTimer(0)
		var lastIndex uint64
		var hasLicenedEver bool

		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}

			licensed, nextIndex, err := checkLisense(lastIndex)
			if err != nil {
				logger.Warn("failed to fetch license", "error", err)
				timer.Reset(1 * time.Minute)
				continue
			}

			if !licensed && hasLicenedEver {
				logger.Warn("automatic snapshot is no longer licensed")
				timer.Reset(1 * time.Hour)
				continue
			}

			if licensed && !hasLicenedEver {
				logger.Info("automatic snapshot is licensed")
				hasLicenedEver = true
				close(licensedCh)
			}

			lastIndex = nextIndex
			timer.Reset(1 * time.Hour)
		}

	}()

	select {
	case <-licensedCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
