package profiles

import (
	_ "embed"
	"encoding/json"
)

//go:embed defaults.json
var defaultsJSON []byte

//go:embed schema.json
var schemaJSON []byte

type Resume struct {
	ResumeMD  string  `json:"resumeMd"`
	Revision  int64   `json:"revision"`
	UpdatedAt *string `json:"updatedAt"`
	MaxLength int     `json:"maxLength"`
}

type Prompt struct {
	Kind      string  `json:"kind"`
	Markdown  string  `json:"markdown"`
	Revision  int64   `json:"revision"`
	UpdatedAt *string `json:"updatedAt"`
	MaxLength int     `json:"maxLength,omitempty"`
}
type PromptCollection struct {
	Prompts    map[string]Prompt `json:"prompts"`
	MaxLength  int               `json:"maxLength"`
	MaxLengths map[string]int    `json:"maxLengths"`
	Defaults   map[string]string `json:"defaults"`
}
type Model struct {
	Kind            string  `json:"kind"`
	Provider        string  `json:"provider"`
	OpenAIModel     string  `json:"openaiModel"`
	OpenRouterModel string  `json:"openrouterModel"`
	ReasoningEffort string  `json:"reasoningEffort"`
	Revision        int64   `json:"revision"`
	UpdatedAt       *string `json:"updatedAt"`
}
type defaultDocument struct {
	Resume  Resume           `json:"resume"`
	Prompts PromptCollection `json:"prompts"`
	Models  map[string]Model `json:"models"`
}

func defaultValues() defaultDocument {
	var d defaultDocument
	if err := json.Unmarshal(defaultsJSON, &d); err != nil {
		panic("invalid embedded source profile defaults")
	}
	return d
}

// Each caller receives its own maps; modifying a response never changes defaults.
func DefaultResume() Resume { return defaultValues().Resume }

func DefaultPrompts() PromptCollection { return defaultValues().Prompts }
func DefaultModels() map[string]Model  { return defaultValues().Models }

type Column struct {
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	NotNull      bool    `json:"notNull"`
	DefaultValue *string `json:"defaultValue"`
	PrimaryKey   bool    `json:"primaryKey"`
}
type StorageTable struct {
	Name         string            `json:"name"`
	Columns      []Column          `json:"columns"`
	ForeignKeys  []json.RawMessage `json:"foreignKeys"`
	ParentTables []string          `json:"parentTables"`
}
type SchemaDocument struct {
	Version int            `json:"version"`
	Tables  []StorageTable `json:"tables"`
}

func Schema() SchemaDocument {
	var schema SchemaDocument
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		panic("invalid embedded source profile schema")
	}
	return schema
}

type Binding struct {
	Table          string `json:"table"`
	KeyColumn      string `json:"keyColumn"`
	KeyValue       any    `json:"keyValue"`
	RevisionColumn string `json:"revisionColumn"`
}

func storageBinding(kind string, model bool) (Binding, error) {
	if !KnownKind(kind) {
		return Binding{}, invalid(404, "Промт не найден.")
	}
	if kind == "cover_letter" || kind == "relevance" {
		table := "system_prompts"
		if model {
			table = "prompt_model_settings"
		}
		return Binding{Table: table, KeyColumn: "kind", KeyValue: kind, RevisionColumn: "revision"}, nil
	}
	table := "vacancy_detail_settings"
	if kind == "vacancy_role" {
		table = "vacancy_role_settings"
	}
	revision := "prompt_revision"
	if model {
		revision = "model_revision"
	}
	return Binding{Table: table, KeyColumn: "id", KeyValue: int64(1), RevisionColumn: revision}, nil
}
func PromptBinding(kind string) (Binding, error) { return storageBinding(kind, false) }
func ModelBinding(kind string) (Binding, error)  { return storageBinding(kind, true) }
