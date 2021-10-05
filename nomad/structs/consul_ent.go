//go:build ent
// +build ent

package structs

func (c *Consul) GetNamespace() string {
	if c == nil {
		return ""
	}
	return c.Namespace
}
