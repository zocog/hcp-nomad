package snapshotagent

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/hcl/v2/hclsimple"
)

type ConsulConfig struct {
	Enabled       *bool  `hcl:"enabled"`
	HTTPAddr      string `hcl:"http_addr,optional"`
	Token         string `hcl:"token,optional"`
	Datacenter    string `hcl:"datacenter,optional"`
	CAFile        string `hcl:"ca_file,optional"`
	CAPath        string `hcl:"ca_path,optional"`
	CertFile      string `hcl:"cert_file,optional"`
	KeyFile       string `hcl:"key_file,optional"`
	TLSServerName string `hcl:"tls_server_name,optional"`
}

type LogConfig struct {
	Level          string `hcl:"level,optional"`
	LogJSON        bool   `hcl:"log_json,optional"`
	EnableSyslog   bool   `hcl:"enable_syslog,optional"`
	SyslogFacility string `hcl:"syslog_facility,optional"`
}

type SnapshotConfig struct {
	IntervalStr        string `hcl:"interval,optional"`
	interval           time.Duration
	Retain             *int   `hcl:"retain,optional"`
	AllowStale         bool   `hcl:"stale,optional"`
	Service            string `hcl:"service,optional"`
	DeregisterAfterStr string `hcl:"deregister_after,optional"`
	deregisterAfter    time.Duration
	LockKey            string `hcl:"lock_key,optional"`
	MaxFailures        *int   `hcl:"max_failures,optional"`
	LocalScratchPath   string `hcl:"local_scratch_path,optional"`
	NamePrefix         string `hcl:"prefix,optional"`
}

type Config struct {
	Consul    *ConsulConfig             `hcl:"consul,block"`
	Log       *LogConfig                `hcl:"log,block"`
	Agent     *SnapshotConfig           `hcl:"snapshot,block"`
	Local     *LocalLibrarianConfig     `hcl:"local_storage,block"`
	S3        *S3LibrarianConfig        `hcl:"aws_storage,block"`
	AzureBlob *AzureBlobLibrarianConfig `hcl:"azure_blob_storage,block"`
	GCS       *GCSLibrarianConfig       `hcl:"google_storage,block"`

	product   string
	cmdConfig cmdFlags
}

func DecodeFile(filename string, input io.Reader, target interface{}) error {
	src, err := ioutil.ReadAll(input)
	if err != nil {
		return err
	}

	return hclsimple.Decode(filename, src, nil, target)
}

func (c *ConsulConfig) toConsulClientConfig(logger hclog.Logger) *api.Config {
	conf := api.DefaultConfigWithLogger(logger)
	if c == nil {
		return conf
	}

	if c.Enabled != nil && !*c.Enabled {
		return nil
	}

	if c.HTTPAddr != "" {
		conf.Address = c.HTTPAddr
	}
	if c.Token != "" {
		conf.Token = c.Token
	}
	if c.Datacenter != "" {
		conf.Datacenter = c.Datacenter
	}
	if c.CAFile != "" {
		conf.TLSConfig.CAFile = c.CAFile
	}
	if c.CAPath != "" {
		conf.TLSConfig.CAPath = c.CAPath
	}
	if c.CertFile != "" {
		conf.TLSConfig.CertFile = c.CertFile
	}
	if c.KeyFile != "" {
		conf.TLSConfig.KeyFile = c.KeyFile
	}
	if c.TLSServerName != "" {
		conf.TLSConfig.Address = c.TLSServerName
	}

	return conf
}

func (c *Config) setDefaults(product string) *Config {
	if c.Log == nil {
		c.Log = &LogConfig{}
	}
	if c.Log.Level == "" {
		c.Log.Level = "INFO"
	}
	if c.Log.SyslogFacility == "" {
		c.Log.SyslogFacility = "LOCAL0"
	}
	if c.Agent == nil {
		c.Agent = &SnapshotConfig{}
	}
	if c.Agent.IntervalStr == "" {
		c.Agent.IntervalStr = "1h"
	}
	if c.Agent.Retain == nil {
		v := 30
		c.Agent.Retain = &v
	}
	if c.Agent.Service == "" {
		c.Agent.Service = product + "-snapshot"
	}
	if c.Agent.DeregisterAfterStr == "" {
		c.Agent.DeregisterAfterStr = "72h"
	}
	if c.Agent.LockKey == "" {
		c.Agent.LockKey = product + "-snapshot/lock"
	}
	if c.Agent.MaxFailures == nil {
		v := 3
		c.Agent.MaxFailures = &v
	}
	if c.Agent.NamePrefix == "" {
		c.Agent.NamePrefix = product
	}
	c.Agent.NamePrefix = strings.TrimRight(c.Agent.NamePrefix, "-")

	if c.Consul == nil {
		falseV := false
		c.Consul = &ConsulConfig{Enabled: &falseV}
	} else if c.Consul.Enabled == nil {
		trueV := true
		c.Consul.Enabled = &trueV
	}

	return c
}

func (c *Config) setDerivedFields() error {
	if dur, err := time.ParseDuration(c.Agent.IntervalStr); err != nil {
		return fmt.Errorf("failed to parse snapshot.interval: %v", err)
	} else {
		c.Agent.interval = dur
	}

	if dur, err := time.ParseDuration(c.Agent.DeregisterAfterStr); err != nil {
		return fmt.Errorf("failed to parse snapshot.interval: %v", err)
	} else {
		c.Agent.deregisterAfter = dur
	}

	return nil
}

func (c *Config) Canonicalize(product string) (*Config, error) {
	c.product = product
	c = c.setDefaults(product)
	c.populateWithFlags()
	err := c.setDerivedFields()
	if err != nil {
		return nil, err
	}
	return c, err
}

func (config *Config) toLibrarian(productPrefix string, logger hclog.Logger) (Librarian, error) {
	defaultKeyPrefix := productPrefix + "-snapshot"

	if config.S3 != nil && config.S3.Bucket != "" {
		if config.S3.Endpoint == "" {
			config.S3.Endpoint = os.Getenv("AWS_S3_ENDPOINT")
		}
		if config.S3.KeyPrefix == "" {
			config.S3.KeyPrefix = defaultKeyPrefix
		}

		librarian, err := NewS3Librarian(config.S3, config.Agent.NamePrefix)
		if err != nil {
			return nil, fmt.Errorf("failed to create S3 librarian: %v", err)
		}
		return librarian, nil
	} else if config.AzureBlob != nil && config.AzureBlob.ContainerName != "" {
		if config.AzureBlob.Environment == "" {
			config.AzureBlob.Environment = "AZUREPUBLICCLOUD"
		}

		// Validate environment
		env, err := azure.EnvironmentFromName(config.AzureBlob.Environment)
		if err != nil {
			return nil, fmt.Errorf("failed to lookup Azure environment: %v", err)
		}
		librarian, err := NewAzureBlobLibrarian(config.AzureBlob, env, config.Agent.NamePrefix, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create Azure Blob librarian: %v", err)
		}
		return librarian, err

	} else if config.GCS != nil && config.GCS.Bucket != "" {
		librarian, err := NewGCSLibrarian(config.GCS, config.Agent.NamePrefix, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create Google Cloud Storage librarian: %v", err)
		}
		return librarian, nil
	} else {
		if config.Local == nil {
			config.Local = &LocalLibrarianConfig{}
		}
		path := config.Local.Path
		if path == "" {
			path = "."
		}
		apath, err := filepath.Abs(config.Local.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to parse snapshot path: %v", path)
		}
		config.Local.Path = apath
		librarian, err := NewLocalLibrarian(config.Local, config.Agent.NamePrefix)
		if err != nil {
			return nil, fmt.Errorf("failed to create local librarian: %v", err)
		}

		// Ensure that the agent uses the local librarian's output path as the
		// scratch space so we don't double-write each snapshot to disk.
		if config.Agent != nil && config.Agent.LocalScratchPath != "" {
			return nil, fmt.Errorf("Local scratch path cannot be configured when using the local librarian")
		}

		config.Agent.LocalScratchPath = config.Local.Path
		return librarian, nil
	}
}
