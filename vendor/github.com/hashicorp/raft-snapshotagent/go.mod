module github.com/hashicorp/raft-snapshotagent

go 1.14

require (
	cloud.google.com/go/storage v1.0.0
	github.com/Azure/azure-storage-blob-go v0.9.0
	github.com/Azure/go-autorest/autorest v0.10.0
	github.com/aws/aws-sdk-go v1.25.41
	github.com/hashicorp/consul/api v1.4.0
	github.com/hashicorp/consul/sdk v0.4.0
	github.com/hashicorp/go-hclog v0.12.0
	github.com/hashicorp/go-syslog v1.0.0
	github.com/hashicorp/go-uuid v1.0.1
	github.com/hashicorp/hcl/v2 v2.5.1
	github.com/hashicorp/raft-snapshot v1.0.2
	github.com/kr/text v0.2.0
	github.com/mitchellh/cli v1.1.0
	github.com/mitchellh/mapstructure v1.3.1
	github.com/rboyer/safeio v0.2.1
	github.com/stretchr/testify v1.5.1
	google.golang.org/api v0.13.0
	google.golang.org/grpc v1.27.1
)
