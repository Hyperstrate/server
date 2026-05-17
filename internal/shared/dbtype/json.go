package dbtype

import (
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// JSONMap is map[string]any serialised as JSON. Atlas/GORM will use jsonb on
// Postgres and text on SQLite — the application layer always sees a plain map.
type JSONMap map[string]any

func (JSONMap) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db.Dialector.Name() == "postgres" {
		return "jsonb"
	}
	return "text"
}

// JSONStringMap is map[string]string serialised as JSON, with the same
// dialect-aware column type as JSONMap.
type JSONStringMap map[string]string

func (JSONStringMap) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db.Dialector.Name() == "postgres" {
		return "jsonb"
	}
	return "text"
}

// JSONStringSlice is []string serialised as JSON, with the same
// dialect-aware column type as JSONMap.
type JSONStringSlice []string

func (JSONStringSlice) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db.Dialector.Name() == "postgres" {
		return "jsonb"
	}
	return "text"
}
