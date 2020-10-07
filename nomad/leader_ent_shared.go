// +build ent

package nomad

// establishProLeadership is used to establish Nomad Pro systems upon acquiring
// leadership.
func (s *Server) establishProLeadership(stopCh chan struct{}) error {
	// Start replication of Namespaces if we are not the authoritative region.
	if s.config.Region != s.config.AuthoritativeRegion {
		go s.replicateNamespaces(stopCh)
	}

	return nil
}

// revokeProLeadership is used to disable Nomad Pro systems upon losing
// leadership.
func (s *Server) revokeProLeadership() error {
	return nil
}
