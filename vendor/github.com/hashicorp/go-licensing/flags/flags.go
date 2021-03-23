package flags

// ProductFlags is an interface to be implemented by enterprise products
// that provides a standardized api for validating  and describing
// product-specific license configurations (aka Flags).
type ProductFlags interface {
	Product() string
	Parse(map[string]interface{}) (interface{}, error)
	DescribeFlags() FlagsOptions
}

// FlagsOptions is a type used to descibe the Flags options
// available for a product.
type FlagsOptions struct {
	Modules  []Module  `json:"modules"`
	Features []Feature `json:"features"`
	Limits   []Limit   `json:"limits"`
}

// Module represents a combinations of features
type Module struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Features    []Feature `json:"features"`
}

type Feature struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Limit represents a scalar limit enforced by the product and
// has a default.
type Limit struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Max         int    `json:"max"`
	Min         int    `json:"min"`
	Default     int    `json:"default"`
}
