package backup

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"itmap/internal/v39"
)

type Repository struct{ DB *sql.DB }
type Download struct {
	Bytes     []byte
	CreatedAt time.Time
}
type sqlColumn struct {
	Name, Type, Extra, Nullable, Key string
	Default, Expression, Collation   sql.NullString
}
type sqlTable struct {
	Name, Engine, Kind string
	Columns            []sqlColumn
}

var migrationNames = []string{"d1_migrations", "__drizzle_migrations", "__appgarden_migrations"}

func quote(name string) string { return "`" + strings.ReplaceAll(name, "`", "``") + "`" }

type metadataReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func inventory(ctx context.Context, tx metadataReader) (map[string]sqlTable, error) {
	result := map[string]sqlTable{}
	rows, err := tx.QueryContext(ctx, `SELECT TABLE_NAME,COALESCE(ENGINE,''),TABLE_TYPE FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() ORDER BY TABLE_NAME`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var t sqlTable
		if err = rows.Scan(&t.Name, &t.Engine, &t.Kind); err != nil {
			rows.Close()
			return nil, err
		}
		result[t.Name] = t
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT TABLE_NAME,COLUMN_NAME,DATA_TYPE,EXTRA,IS_NULLABLE,COLUMN_KEY,COLUMN_DEFAULT,GENERATION_EXPRESSION,COLLATION_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() ORDER BY TABLE_NAME,ORDINAL_POSITION`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var c sqlColumn
		if err = rows.Scan(&name, &c.Name, &c.Type, &c.Extra, &c.Nullable, &c.Key, &c.Default, &c.Expression, &c.Collation); err != nil {
			return nil, err
		}
		t, ok := result[name]
		if !ok {
			return nil, invalid("Структура базы изменилась во время экспорта.")
		}
		t.Columns = append(t.Columns, c)
		result[name] = t
	}
	return result, rows.Err()
}
func validateInventory(actual map[string]sqlTable, trusted v39.Schema) error {
	definitions := map[string]v39.Table{}
	for _, t := range append(trusted.Tables, trusted.OperationalTables...) {
		definitions[t.Name] = t
	}
	migrations := map[string]bool{}
	for _, name := range migrationNames {
		migrations[name] = true
	}
	for name, t := range actual {
		if t.Kind != "BASE TABLE" || t.Engine != "InnoDB" {
			return invalid("Экспорт требует поддерживаемые таблицы InnoDB.")
		}
		if migrations[name] {
			continue
		}
		d, known := definitions[name]
		if !known {
			return invalid("Обнаружена неизвестная таблица. Экспорт остановлен, чтобы не потерять данные.")
		}
		columns := map[string]string{}
		for _, c := range t.Columns {
			if (name == "scheduler_jobs" && c.Name == "_d1_active_scan") || (name == "vacancy_roles" && c.Name == "_d1_ascii_nocase_name") {
				if !strings.Contains(c.Extra, "VIRTUAL GENERATED") || !strings.Contains(c.Extra, "INVISIBLE") || !c.Expression.Valid || c.Expression.String == "" {
					return invalid("Неверная структура служебного столбца.")
				}
				continue
			}
			if strings.Contains(c.Extra, "INVISIBLE") || strings.Contains(c.Extra, "GENERATED") {
				return invalid("Неизвестный служебный столбец. Экспорт остановлен.")
			}
			columns[c.Name] = c.Type
		}
		if len(columns) != len(d.Columns) {
			return invalid("Неполная или несовместимая структура раздела.")
		}
		for _, c := range d.Columns {
			kind := columns[c.Name]
			if kind == "" || c.Type == "INTEGER" && kind != "bigint" || c.Type == "REAL" && kind != "double" || c.Type == "TEXT" && kind != "longtext" && kind != "varchar" && kind != "text" {
				return invalid("Неполная или несовместимая структура раздела.")
			}
		}
	}
	return nil
}
func sqlScalar(value any, kind string) (json.RawMessage, error) {
	if value == nil {
		return json.RawMessage("null"), nil
	}
	var raw []byte
	switch v := value.(type) {
	case int64:
		if v > maxSafeInteger || v < -maxSafeInteger {
			return nil, invalid("Целое число не представимо без потери точности в v39.")
		}
		raw = strconv.AppendInt(nil, v, 10)
	case uint64:
		if v > maxSafeInteger {
			return nil, invalid("Целое число не представимо без потери точности в v39.")
		}
		raw = strconv.AppendUint(nil, v, 10)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, invalid("Некорректное числовое значение.")
		}
		raw, _ = json.Marshal(v)
	case []byte:
		if !utf8.Valid(v) {
			return nil, invalid("Текст базы не является корректным UTF-8.")
		}
		if kind == "bigint" || kind == "int" || kind == "tinyint" {
			n, e := strconv.ParseInt(string(v), 10, 64)
			if e != nil {
				return nil, invalid("Непредставимое целое значение.")
			}
			return sqlScalar(n, kind)
		}
		if kind == "double" {
			n, e := strconv.ParseFloat(string(v), 64)
			if e != nil {
				return nil, invalid("Непредставимое числовое значение.")
			}
			return sqlScalar(n, kind)
		}
		return encodeScalarText(string(v)), nil
	case string:
		return sqlScalar([]byte(v), kind)
	default:
		return nil, invalid("Неподдерживаемый тип значения базы.")
	}
	return raw, nil
}
func encodeScalarText(s string) json.RawMessage {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(s)
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}

type exportBudget struct{ bytes, rows int }

func readRows(ctx context.Context, tx *sql.Tx, t sqlTable, names []string, budget *exportBudget, application bool) ([]json.RawMessage, int64, error) {
	return readRowsMode(ctx, tx, t, names, budget, application, false)
}

func readRowsMode(ctx context.Context, tx *sql.Tx, t sqlTable, names []string, budget *exportBudget, application, locking bool) ([]json.RawMessage, int64, error) {
	columns := map[string]sqlColumn{}
	for _, c := range t.Columns {
		columns[c.Name] = c
	}
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = quote(n)
	}
	query := "SELECT " + strings.Join(quoted, ",") + " FROM " + quote(t.Name)
	if locking {
		query += " FOR UPDATE"
	}
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := make([]json.RawMessage, 0)
	var maxID int64
	for rows.Next() {
		if application {
			budget.rows++
			if budget.rows > MaxRows {
				return nil, 0, invalid("В бэкапе слишком много записей.")
			}
		}
		values := make([]any, len(names))
		pointers := make([]any, len(names))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err = rows.Scan(pointers...); err != nil {
			return nil, 0, err
		}
		out := []byte{'{'}
		for i, n := range names {
			v, e := sqlScalar(values[i], columns[n].Type)
			if e != nil {
				return nil, 0, e
			}
			if i > 0 {
				out = append(out, ',')
			}
			key, _ := json.Marshal(n)
			out = append(out, key...)
			out = append(out, ':')
			out = append(out, v...)
			if n == "id" && columns[n].Type == "bigint" {
				if id, e := strconv.ParseInt(string(v), 10, 64); e == nil && id > maxID {
					maxID = id
				}
			}
		}
		out = append(out, '}')
		budget.bytes += len(out)
		if budget.bytes > MaxBytes {
			return nil, 0, &InputError{Status: 413, Message: "Полный бэкап превышает 25 МБ."}
		}
		result = append(result, out)
	}
	return result, maxID, rows.Err()
}

// Export uses one read-only repeatable-read transaction. AUTO_INCREMENT is
// non-MVCC MariaDB metadata: its conservative high-water is sampled after rows,
// and never below a snapshot ID. It can include concurrent/rolled-back gaps.
// Informational metadata.schema is trusted logical SQLite DDL; physical MariaDB
// columns/engines are validated separately before and after the snapshot.
func (r Repository) Export(ctx context.Context) (Download, error) {
	if r.DB == nil {
		return Download{}, errors.New("export storage unavailable")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return Download{}, err
	}
	defer tx.Rollback()
	before, err := inventory(ctx, tx)
	if err != nil {
		return Download{}, err
	}
	trusted := v39.Trusted()
	if err = validateInventory(before, trusted); err != nil {
		return Download{}, err
	}
	type storageTable struct {
		Name    string            `json:"name"`
		Columns []v39.Column      `json:"columns"`
		Rows    []json.RawMessage `json:"rows"`
	}
	tables := make([]storageTable, 0, len(trusted.Tables))
	missing := []string{}
	budget := &exportBudget{}
	maxIDs := map[string]int64{}
	catalog := v39.DefaultCatalog()
	for _, definition := range trusted.Tables {
		rows := []json.RawMessage{}
		var maxID int64
		if physical, ok := before[definition.Name]; ok {
			names := make([]string, len(definition.Columns))
			for i, c := range definition.Columns {
				names[i] = c.Name
			}
			rows, maxID, err = readRows(ctx, tx, physical, names, budget, true)
			if err != nil {
				return Download{}, err
			}
		} else {
			missing = append(missing, definition.Name)
		}
		maxIDs[definition.Name] = maxID
		tables = append(tables, storageTable{definition.Name, definition.Columns, rows})
		if definition.Name == "app_catalog" {
			for _, row := range rows {
				node, e := orderedJSON(row)
				if e != nil {
					return Download{}, e
				}
				id := node.field("id")
				if id != nil && id.kind == 'f' && id.number == 1 {
					cell := node.field("data_json")
					if cell == nil || cell.kind != 's' {
						return Download{}, invalid("Сохранённый каталог повреждён.")
					}
					text, e := jsonSourceText(cell.text)
					if e != nil {
						return Download{}, e
					}
					c, e := orderedJSON(text)
					if e != nil || !validCatalog(c) {
						return Download{}, invalid("Сохранённый каталог повреждён.")
					}
					catalog = c.stringify(nil, false)
					break
				}
			}
		}
	}
	type migration struct {
		Name string            `json:"name"`
		Rows []json.RawMessage `json:"rows"`
	}
	migrations := []migration{}
	for _, name := range migrationNames {
		if physical, ok := before[name]; ok {
			names := []string{}
			for _, c := range physical.Columns {
				names = append(names, c.Name)
			}
			rows, _, e := readRows(ctx, tx, physical, names, budget, false)
			if e != nil {
				return Download{}, e
			}
			migrations = append(migrations, migration{name, rows})
		}
	}
	sequences := []Sequence{}
	for _, o := range trusted.Objects {
		if o.Type != "table" || !strings.Contains(strings.ToUpper(o.SQL), "AUTOINCREMENT") {
			continue
		}
		if _, ok := before[o.Name]; !ok {
			continue
		}
		var next sql.NullString
		if err = tx.QueryRowContext(ctx, `SELECT AUTO_INCREMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=?`, o.Name).Scan(&next); err != nil {
			return Download{}, err
		}
		if !next.Valid {
			return Download{}, invalid("Недоступен счётчик идентификаторов.")
		}
		n, e := strconv.ParseUint(next.String, 10, 64)
		if e != nil || n == 0 || n > maxSafeInteger+1 {
			return Download{}, invalid("Непредставимый счётчик идентификаторов.")
		}
		seq := int64(n - 1)
		if maxIDs[o.Name] > seq {
			seq = maxIDs[o.Name]
		}
		if seq > 0 {
			sequences = append(sequences, Sequence{o.Name, float64(seq)})
		}
	}
	after, err := inventory(ctx, tx)
	if err != nil {
		return Download{}, err
	}
	if !reflect.DeepEqual(before, after) {
		return Download{}, invalid("Структура базы изменилась во время экспорта. Повторите скачивание.")
	}
	objects := []v39.Object{}
	for _, o := range trusted.Objects {
		if _, ok := before[o.Table]; ok {
			objects = append(objects, o)
		}
	}
	sort.Slice(objects, func(i, j int) bool {
		if objects[i].Type != objects[j].Type {
			return objects[i].Type < objects[j].Type
		}
		return objects[i].Name < objects[j].Name
	})
	now := time.Now().UTC()
	payload := struct {
		Format    string          `json:"format"`
		Version   int             `json:"version"`
		CreatedAt string          `json:"createdAt"`
		Catalog   json.RawMessage `json:"catalog"`
		Storage   struct {
			Tables []storageTable `json:"tables"`
		} `json:"storage"`
		Manifest []ManifestEntry `json:"manifest"`
		Metadata struct {
			Schema        []v39.Object `json:"schema"`
			Sequences     []Sequence   `json:"sequences"`
			Migrations    []migration  `json:"migrations"`
			MissingTables []string     `json:"missingTables"`
		} `json:"metadata"`
	}{Format: Format, Version: Version, CreatedAt: now.Format("2006-01-02T15:04:05.000Z"), Catalog: catalog}
	payload.Storage.Tables = tables
	payload.Manifest = []ManifestEntry{}
	payload.Metadata.Schema = objects
	payload.Metadata.Sequences = sequences
	payload.Metadata.Migrations = migrations
	payload.Metadata.MissingTables = missing
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err = encoder.Encode(payload); err != nil {
		return Download{}, fmt.Errorf("encode export: %w", err)
	}
	output, err := EncodeV39(encoded.Bytes())
	if err != nil {
		return Download{}, err
	}
	if err = tx.Commit(); err != nil {
		return Download{}, err
	}
	return Download{Bytes: output, CreatedAt: now}, nil
}
