//go:build ent
// +build ent

package structs

func (c *Consul) GetNamespace() string {
	if c == nil {
		return ""
	}
	return c.Namespace
}

// GetConsulClusterName gets the cluster for this task. The cluster field will
// always be set on any job submitted after 1.7, so for any older job we can
// safely fallback to default without worrying about the ENT namespace control
// over access to multiple clusters.
func (t *Task) GetConsulClusterName(tg *TaskGroup) string {
	if t.Consul != nil && t.Consul.Cluster != "" {
		return t.Consul.Cluster
	}
	if tg != nil && tg.Consul != nil && tg.Consul.Cluster != "" {
		return tg.Consul.Cluster
	}
	return ConsulDefaultCluster
}

// GetConsulClusterName gets the cluster for this service. The cluster field
// will always be set on any job submitted after 1.7, so for any older job we
// can safely fallback to default without worrying about the ENT namespace
// control over access to multiple clusters.
func (s *Service) GetConsulClusterName(tg *TaskGroup) string {
	if !s.IsConsul() {
		return ""
	}
	if s.Cluster != "" {
		return s.Cluster
	}
	if s.TaskName != "" {
		task := tg.LookupTask(s.TaskName)
		if task != nil && task.Consul != nil && task.Consul.Cluster != "" {
			return task.Consul.Cluster
		}
	}
	if tg != nil && tg.Consul != nil && tg.Consul.Cluster != "" {
		return tg.Consul.Cluster
	}
	return ConsulDefaultCluster
}
