package sentinel

import "fmt"

// ParamSchema defines expected data for a parameter. This can assist
// tools and integrations that require this information in order to
// prompt for values or understand what policies expect a value.
type ParamSchema struct {
	// The name of the parameter.
	Name string

	// The policy that this parameter applies to. This will correspond
	// to the ID of the policy, or in other words, the friendly name,
	// which may be different than the actual name of the file on disk.
	//
	// When namespacing a parameter, this is the name that must precede
	// the slash in the format POLICY_NAME/KEY.
	Policy string `json:"policy"`

	// Denotes if the value is required, true if it is.
	Required bool `json:"required,omitempty"`

	// The description of the parameter if it exists.
	Description string `json:"description,omitempty"`

	// File location data for the parameter.
	Position ParamSchemaFilePosition `json:"position"`
}

// ParamSchemaFilePosition provides file location data for a
// parameter. This is a copy of token.Position.
type ParamSchemaFilePosition struct {
	Filename string `json:"filename"` // filename, if any
	Offset   int    `json:"offset"`   // offset, starting at 0
	Line     int    `json:"line"`     // line number, starting at 1
	Column   int    `json:"column"`   // column number, starting at 1 (byte count)
}

// IsValid reports whether the position is valid.
func (pos *ParamSchemaFilePosition) IsValid() bool { return pos.Line > 0 }

// String returns a string in one of several forms:
//
//	file:line:column    valid position with file name
//	line:column         valid position without file name
//	file                invalid position with file name
//	-                   invalid position without file name
//
func (pos ParamSchemaFilePosition) String() string {
	s := pos.Filename
	if pos.IsValid() {
		if s != "" {
			s += ":"
		}
		s += fmt.Sprintf("%d:%d", pos.Line, pos.Column)
	}
	if s == "" {
		s = "-"
	}
	return s
}

// ParamSchemas returns a map of schemas for all parameters found in
// the supplied policy set.
//
// The output is defined by ParamSchema. Common parameters are
// grouped together - the policies that use the actual parameter can
// be seen in the policies field.
//
// This function should only be used after all policies in the
// supplied set are loaded for a policy set and set to ready.
func ParamSchemas(policies []*Policy) []ParamSchema {
	result := make([]ParamSchema, 0)
	for _, policy := range policies {
		for _, param := range policy.File().Params {
			pos := policy.FileSet().Position(param.Pos())
			result = append(result, ParamSchema{
				Name:        param.Name.Name,
				Policy:      policy.Name(),
				Required:    param.Default == nil,
				Description: param.Doc.Text(),
				Position: ParamSchemaFilePosition{
					Filename: pos.Filename,
					Offset:   pos.Offset,
					Line:     pos.Line,
					Column:   pos.Column,
				},
			})
		}
	}

	return result
}
