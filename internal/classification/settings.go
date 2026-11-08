// Package classification хранит общие направления отбора заголовков.
// Чтение не создаёт ролей, не вызывает JEV и не запускает парсер.
package classification

import (
	"context"
	"database/sql"
	"errors"
	"unicode/utf8"

	"itmap/internal/roles"
)

const MaxPromptLength = 10000
const MaxSafeRevision int64 = 9007199254740991

var (
	ErrStorage = errors.New("classification settings storage unavailable")
	ErrConflict = errors.New("classification settings changed or root missing")
	ErrInput = errors.New("invalid classification settings input")
)

const managerDefault = `Определи по названию вакансии, относится ли она к управлению IT-разработкой, IT-проектами, цифровыми продуктами или инженерными командами.

Подходящие направления:
- Руководитель разработки, Head of Development.
- Руководитель инженерной команды, Head of Engineering.
- Engineering Manager, Development Manager.
- Руководитель IT-проектов, Senior IT Project Manager.
- Technical Project Manager, Technical Program Manager.
- Program Manager, Program Director.
- Delivery Manager, Head of Delivery.
- Product Manager, Senior Product Manager.
- Head of Product, Product Lead, Product Owner.
- Лидер продукта, владелец продукта, руководитель продукта, руководитель продуктового направления.
- Platform Product Manager.
- Другие близкие управленческие роли, связанные с разработкой программного обеспечения, IT-проектами и цифровыми продуктами.

Не подходят:
- Рядовые разработчики без управленческих обязанностей.
- Тестировщики и QA без руководящих функций.
- Системные администраторы и DevOps без управления разработкой.
- Дизайнеры без управления цифровым продуктом.
- Менеджеры проектов, не связанных с IT.
- Менеджеры продаж, маркетинга и HR.
- Административные руководители вне IT.

Правила:
1. Оценивай смысл названия, а не буквальное совпадение слов.
2. Русские и английские названия равнозначны.
3. Не придумывай обязанности, которых нет в заголовке. Название «лидер продукта» означает продуктовую управленческую роль и не требует буквальных слов IT или Manager. Не отклоняй цифровой продукт только потому, что в заголовке нет слова «разработка».
4. Если название неоднозначно, но потенциально относится к управлению IT-разработкой, IT-проектами или цифровыми продуктами, выбирай «Подходит».
5. Не оценивай зарплату, работодателя или опыт кандидата. На этом этапе используется только заголовок вакансии.

Результат классификации: match или reject.`
const journalistDefault = `Определи по названию вакансии, относится ли она к профессиональной журналистике, редакторской работе или созданию журналистских материалов.

Подходящие направления:
- Редактор.
- Выпускающий редактор.
- Шеф-редактор.
- Главный редактор.
- Редактор новостей.
- Редактор СМИ.
- Редактор медиа.
- Журналист.
- Корреспондент.
- Экономический журналист.
- Деловой журналист.
- Дата-журналист, Data Journalist.
- Журналист-аналитик.
- Другие близкие специальности, связанные с профессиональной журналистикой и редакционной работой.

Не подходят:
- Чистый рекламный копирайтинг.
- SMM и продвижение в социальных сетях без журналистской составляющей.
- PR и корпоративные коммуникации без редакционной работы.
- Маркетинговые должности.
- Создание рекламных текстов без журналистских задач.
- Другие должности, не связанные с журналистикой и редактурой.

Правила:
1. Оценивай смысл названия.
2. Учитывай русские и английские эквиваленты.
3. Не придумывай дополнительные обязанности.
4. Если название неоднозначно, но потенциально относится к профессиональной журналистике или редакторской работе, выбирай «Подходит».
5. Не оценивай зарплату, работодателя и квалификацию кандидата. Используй только заголовок вакансии.

Результат классификации: match или reject.`

func DefaultForKey(key string) *string {
	var result string
	switch key {
	case "root-it-manager":
		result = managerDefault
	case "root-journalist":
		result = journalistDefault
	default:
		return nil
	}
	return &result
}

// RoleSettings соответствует React ClassificationRole и существующему D1 API.
// DefaultMarkdown отделён от сохранённого Markdown: пустой пользовательский
// промт нельзя восстанавливать неявно во время обычного GET.
type RoleSettings struct {
	RoleID int64 `json:"roleId"`
	Name string `json:"name"`
	Markdown string `json:"markdown"`
	Revision int64 `json:"revision"`
	UpdatedAt string `json:"updatedAt"`
	Enabled bool `json:"enabled"`
	DefaultMarkdown *string `json:"defaultMarkdown"`
	Descendants []Descendant `json:"descendants"`
}

type Descendant struct {
	ID int64 `json:"id"`
	Path string `json:"path"`
}

type SettingsView struct {
	MaxLength int `json:"maxLength"`
	Roles []RoleSettings `json:"roles"`
}

// SavedRole имеет необходимые поля для немедленного обновления UI после PUT.
type SavedRole struct {
	RoleID int64 `json:"roleId"`
	Name string `json:"name"`
	Markdown string `json:"markdown"`
	Revision int64 `json:"revision"`
	UpdatedAt string `json:"updatedAt"`
	Enabled bool `json:"enabled"`
}

type Repository struct{ DB *sql.DB }

func (r Repository) Read(ctx context.Context) (SettingsView, error) {
	view := SettingsView{MaxLength: MaxPromptLength, Roles: []RoleSettings{}}
	if r.DB == nil {
		return view, ErrStorage
	}
	catalog, err := (roles.Repository{DB: r.DB}).Read(ctx)
	if err != nil {
		return view, ErrStorage
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT role_id,markdown,revision,parsing_enabled,updated_at FROM vacancy_role_classification`)
	if err != nil {
		return view, ErrStorage
	}
	saved := make(map[int64]RoleSettings)
	for rows.Next() {
		var item RoleSettings
		var enabled int64
		if err = rows.Scan(&item.RoleID, &item.Markdown, &item.Revision, &enabled, &item.UpdatedAt); err != nil {
			break
		}
		if item.Revision < 0 || item.Revision > MaxSafeRevision || (enabled != 0 && enabled != 1) || !utf8.ValidString(item.Markdown) {
			err = ErrStorage
			break
		}
		item.Enabled = enabled == 1
		saved[item.RoleID] = item
	}
	if err == nil {
		err = rows.Err()
	}
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return view, ErrStorage
	}
	byID := make(map[int64]roles.Role, len(catalog.Roles))
	for _, role := range catalog.Roles {
		byID[role.ID] = role
	}
	for _, root := range catalog.Roots {
		item, exists := saved[root.ID]
		if !exists {
			item = RoleSettings{RoleID: root.ID, Markdown: "", Revision: 0, Enabled: false}
		}
		item.Name = root.Name
		item.DefaultMarkdown = DefaultForKey(root.StableKey)
		item.Descendants = []Descendant{}
		for _, id := range root.DescendantIDs {
			role, ok := byID[id]
			if ok {
				item.Descendants = append(item.Descendants, Descendant{ID: role.ID, Path: role.Path})
			}
		}
		view.Roles = append(view.Roles, item)
	}
	return view, nil
}

func validRevision(value int64) bool { return value >= 0 && value < MaxSafeRevision }

func (r Repository) SaveEnabled(ctx context.Context, rootID, revision int64, enabled bool) (SavedRole, error) {
	return r.save(ctx, rootID, revision, &enabled, nil)
}

func (r Repository) SavePrompt(ctx context.Context, rootID, revision int64, markdown string) (SavedRole, error) {
	if !utf8.ValidString(markdown) || utf8.RuneCountInString(markdown) > MaxPromptLength {
		return SavedRole{}, ErrInput
	}
	return r.save(ctx, rootID, revision, nil, &markdown)
}

func (r Repository) save(ctx context.Context, rootID, revision int64, enabled *bool, markdown *string) (SavedRole, error) {
	var result SavedRole
	if rootID <= 0 || rootID > MaxSafeRevision || !validRevision(revision) || (enabled == nil && markdown == nil) || (enabled != nil && markdown != nil) {
		return result, ErrInput
	}
	if r.DB == nil {
		return result, ErrStorage
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return result, ErrStorage
	}
	defer tx.Rollback()
	// Лок на корне сериализует и первую вставку строки, и обычные CAS-правки.
	if err = tx.QueryRowContext(ctx, "SELECT name FROM vacancy_roles WHERE id=? AND parent_id IS NULL FOR UPDATE", rootID).Scan(&result.Name); err != nil {
		if errors.Is(err, sql.ErrNoRows) { return result, ErrConflict }
		return result, ErrStorage
	}
	var storedRevision int64
	err = tx.QueryRowContext(ctx, "SELECT revision FROM vacancy_role_classification WHERE role_id=? FOR UPDATE", rootID).Scan(&storedRevision)
	if errors.Is(err, sql.ErrNoRows) {
		if revision != 0 { return result, ErrConflict }
		var initial string
		var active int64
		if markdown != nil { initial = *markdown }
		if enabled != nil && *enabled { active = 1 }
		_, err = tx.ExecContext(ctx,
			"INSERT INTO vacancy_role_classification(role_id,markdown,revision,parsing_enabled,updated_at) VALUES(?,?,1,?,CURRENT_TIMESTAMP)",
			rootID, initial, active)
		if err != nil { return result, ErrStorage }
	} else {
		if err != nil { return result, ErrStorage }
		if storedRevision != revision { return result, ErrConflict }
		var res sql.Result
		if enabled != nil {
			active := int64(0)
			if *enabled { active = 1 }
			res, err = tx.ExecContext(ctx,
				"UPDATE vacancy_role_classification SET parsing_enabled=?,revision=revision+1,updated_at=CURRENT_TIMESTAMP WHERE role_id=? AND revision=?",
				active, rootID, revision)
		} else {
			res, err = tx.ExecContext(ctx,
				"UPDATE vacancy_role_classification SET markdown=?,revision=revision+1,updated_at=CURRENT_TIMESTAMP WHERE role_id=? AND revision=?",
				*markdown, rootID, revision)
		}
		if err != nil { return result, ErrStorage }
		affected, e := res.RowsAffected()
		if e != nil { return result, ErrStorage }
		if affected != 1 { return result, ErrConflict }
	}
	var active int64
	if err = tx.QueryRowContext(ctx,
		"SELECT markdown,revision,parsing_enabled,updated_at FROM vacancy_role_classification WHERE role_id=?",
		rootID).Scan(&result.Markdown, &result.Revision, &active, &result.UpdatedAt); err != nil {
		return result, ErrStorage
	}
	result.RoleID = rootID
	result.Enabled = active == 1
	if err = tx.Commit(); err != nil { return SavedRole{}, ErrStorage }
	return result, nil
}
