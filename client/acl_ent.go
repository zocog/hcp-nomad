//go:build ent
// +build ent

package client

import "github.com/hashicorp/nomad/nomad/structs"

// ResolveIdentity exists only to let the enterprise audit logger get
// AuthenticatedIdentity structs instead of precompiled acl.ACL structs.
//
// TODO https://github.com/hashicorp/nomad-enterprise/issues/1045
func (c *Client) ResolveIdentity(bearerToken string) (*structs.AuthenticatedIdentity, error) {
	return c.resolveTokenValue(bearerToken)
}
