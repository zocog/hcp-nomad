package snapshotagent

import (
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/hashicorp/go-hclog"
)

// AzureBlobLibrarianConfig configures an AzureBlob librarian.
type AzureBlobLibrarianConfig struct {
	AccountName   string `hcl:"account_name,optional"`
	AccountKey    string `hcl:"account_key,optional"`
	ContainerName string `hcl:"container_name,optional"`
	Environment   string `hcl:"environment,optional"`
}

// AzureBlobLibrarian stores snapshots in Azure's Blob Storage.
type AzureBlobLibrarian struct {
	// config is the configuration of the librarian as given at create time.
	config      *AzureBlobLibrarianConfig
	environment azure.Environment
	logger      hclog.Logger

	outputNamePrefix string
}
