package schemamigrator

// StaticDDLOperation creates one object from the frozen v1 local schema.
type StaticDDLOperation struct {
	Database string
	Table    string
	Query    string
}

func (s StaticDDLOperation) IsMutation() bool    { return false }
func (s StaticDDLOperation) IsIdempotent() bool  { return true }
func (s StaticDDLOperation) IsLightweight() bool { return true }
func (s StaticDDLOperation) ForceMigrate() bool  { return false }

func (s StaticDDLOperation) ToSQL() string { return s.Query }
