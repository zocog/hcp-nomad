// +build !ent

// TODO: OSS PR!!!

package client

type EnterpriseClient struct{}

func newEnterpriseClient() *EnterpriseClient {
	return &EnterpriseClient{}
}

func (ec *EnterpriseClient) SetFeatures(features uint64) {
	return
}
