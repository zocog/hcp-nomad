package semantic

import (
	"bytes"
	"fmt"

	"github.com/hashicorp/sentinel/lang/token"
)

// CheckError is the error type that is returned for semantic check failures.
type CheckError struct {
	Type    CheckType // Unique string for the type of error
	FileSet *token.FileSet
	Pos     token.Pos
	Message string
}

// CheckType is an enum of the various types of check errors that can exist.
// A single check type may correspond to different error messages but
// represents a broad category of similar errors.
type CheckType string

const (
	CheckTypeNoMain                 CheckType = "no-main"
	CheckTypeBreak                            = "break"
	CheckTypeImportBadPath                    = "import-bad-path"
	CheckTypeImportIdentRequired              = "import-ident-required"
	CheckTypeImportSelectorRequired           = "import-selector-required"
	CheckTypeImportAssignProhibited           = "import-assign-prohibited"
	CheckTypeAppendAssign                     = "append-assign"
)

func (e *CheckError) Error() string {
	var buf bytes.Buffer

	if e.Pos.IsValid() {
		buf.WriteString(fmt.Sprintf("%s: ", e.FileSet.Position(e.Pos)))
	}

	buf.WriteString(e.Message)
	return buf.String()
}
