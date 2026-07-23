package schemamigrator

import "strings"

type CreateProjectionOperation struct {
	cluster    string
	Database   string
	Table      string
	Projection Projection
}

func (c CreateProjectionOperation) IsMutation() bool {
	// Create projection is not a mutation.
	return false
}

func (c CreateProjectionOperation) IsIdempotent() bool {
	// Create projection is idempotent. It will not change the table if the projection already exists.
	return true
}

func (c CreateProjectionOperation) IsLightweight() bool {
	// Create projection is lightweight.
	return true
}

func (c CreateProjectionOperation) ForceMigrate() bool {
	return false
}

func (c CreateProjectionOperation) ToSQL() string {
	var sql strings.Builder
	sql.WriteString("ALTER TABLE ")
	sql.WriteString(c.Database)
	sql.WriteString(".")
	sql.WriteString(c.Table)
	sql.WriteString(" ADD PROJECTION IF NOT EXISTS ")
	sql.WriteString(c.Projection.Name)
	sql.WriteString(" (")
	sql.WriteString(c.Projection.Query)
	sql.WriteString(")")
	return sql.String()
}

type DropProjectionOperation struct {
	cluster    string
	Database   string
	Table      string
	Projection Projection
}

func (d DropProjectionOperation) IsMutation() bool {
	// Drop projection is not a mutation.
	return false
}

func (d DropProjectionOperation) IsIdempotent() bool {
	// Drop projection is idempotent. It will not change the table if the projection does not exist.
	return true
}

func (d DropProjectionOperation) IsLightweight() bool {
	// Drop projection is lightweight.
	return true
}

func (d DropProjectionOperation) ForceMigrate() bool {
	return false
}

func (d DropProjectionOperation) ToSQL() string {
	var sql strings.Builder
	sql.WriteString("ALTER TABLE ")
	sql.WriteString(d.Database)
	sql.WriteString(".")
	sql.WriteString(d.Table)
	sql.WriteString(" DROP PROJECTION IF EXISTS ")
	sql.WriteString(d.Projection.Name)
	return sql.String()
}
