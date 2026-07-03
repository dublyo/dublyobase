package core

// Field is a single column in a collection. Each field type encapsulates its
// own Postgres column definition and (later) (de)serialization + validation,
// so adapting the data model to Postgres is "implement ColumnType" per type —
// the extension point borrowed from PocketBase's Field abstraction.
type Field interface {
	Type() string
	Name() string
	// ColumnType returns the Postgres column definition, e.g. "text not null default ''".
	ColumnType() string
}

type baseField struct {
	FieldName string `json:"name"`
	Required  bool   `json:"required"`
}

func (f baseField) Name() string { return f.FieldName }

// TextField -> text
type TextField struct{ baseField }

func (TextField) Type() string { return "text" }
func (f TextField) ColumnType() string {
	if f.Required {
		return "text not null default ''"
	}
	return "text"
}

// NumberField -> double precision
type NumberField struct{ baseField }

func (NumberField) Type() string       { return "number" }
func (NumberField) ColumnType() string { return "double precision" }

// BoolField -> boolean
type BoolField struct{ baseField }

func (BoolField) Type() string       { return "bool" }
func (BoolField) ColumnType() string { return "boolean not null default false" }

// DateField -> timestamptz (native, not PocketBase's UTC strings)
type DateField struct{ baseField }

func (DateField) Type() string       { return "date" }
func (DateField) ColumnType() string { return "timestamptz" }

// JSONField -> jsonb
type JSONField struct{ baseField }

func (JSONField) Type() string       { return "json" }
func (JSONField) ColumnType() string { return "jsonb not null default '{}'::jsonb" }

// compile-time checks that every field type satisfies Field.
var (
	_ Field = TextField{}
	_ Field = NumberField{}
	_ Field = BoolField{}
	_ Field = DateField{}
	_ Field = JSONField{}
)
