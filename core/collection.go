package core

// CollectionType distinguishes the three kinds of collections.
type CollectionType string

const (
	CollectionBase CollectionType = "base"
	CollectionAuth CollectionType = "auth"
	CollectionView CollectionType = "view"
)

// Collection is a schema definition stored as a row in the _dbo.collections
// meta-table; each base/auth collection materializes a real Postgres table.
//
// Access rules compile to SQL WHERE fragments AND native Postgres RLS policies:
//
//	nil  -> superuser only
//	""   -> public
//	expr -> compiled rule
type Collection struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Type   CollectionType `json:"type"`
	System bool           `json:"system"`
	Fields []Field        `json:"fields"`

	ListRule   *string `json:"listRule"`
	ViewRule   *string `json:"viewRule"`
	CreateRule *string `json:"createRule"`
	UpdateRule *string `json:"updateRule"`
	DeleteRule *string `json:"deleteRule"`
}
