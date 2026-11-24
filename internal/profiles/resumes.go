package profiles

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrResumeSelection = errors.New("invalid resume selection")

type NamedResume struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Markdown string `json:"markdown"`
	Version  int64  `json:"-"`
}

type Resumes struct {
	Primary        int           `json:"primary"`
	Revision       int64         `json:"revision"`
	LegacyRevision int64         `json:"legacyRevision"`
	Items          []NamedResume `json:"items"`
}

type resumeNames struct {
	Primary int      `json:"primary"`
	Names   []string `json:"names"`
}

// Метаданные и тексты используют действующую таблицу профилей и её резервное копирование.
const ResumesMetadataPrefix = "resumes:"
const Resume2Prefix = "resume2:"
const Resume3Prefix = "resume3:"

func readResumes(ctx context.Context, q profileRowQueryer, user string) (Resumes, error) {
	var out Resumes
	var metadata string
	var texts [3]string
	var versions [3]int64
	err := q.QueryRowContext(ctx, `SELECT COALESCE(m.resume_md,''),COALESCE(m.revision,0),
		COALESCE(r.resume_md,''),COALESCE(r.revision,0),COALESCE(r2.resume_md,''),COALESCE(r2.revision,0),COALESCE(r3.resume_md,''),COALESCE(r3.revision,0)
		FROM (SELECT ? AS user_name) u
		LEFT JOIN user_profiles m ON m.user_name=CONCAT('resumes:',u.user_name)
		LEFT JOIN user_profiles r ON r.user_name=u.user_name
		LEFT JOIN user_profiles r2 ON r2.user_name=CONCAT('resume2:',u.user_name)
		LEFT JOIN user_profiles r3 ON r3.user_name=CONCAT('resume3:',u.user_name)`, user).
		Scan(&metadata, &out.Revision, &texts[0], &out.LegacyRevision, &texts[1], &versions[1], &texts[2], &versions[2])
	if err != nil {
		return Resumes{}, err
	}
	names := resumeNames{Primary: 1, Names: []string{"Основное резюме"}}
	if metadata != "" {
		if json.Unmarshal([]byte(metadata), &names) != nil || len(names.Names) < 1 || len(names.Names) > 3 || names.Primary < 1 || names.Primary > len(names.Names) {
			return Resumes{}, ErrUnavailable
		}
	}
	versions[0] = out.LegacyRevision
	out.Primary = names.Primary
	out.Items = make([]NamedResume, len(names.Names))
	for i, name := range names.Names {
		out.Items[i] = NamedResume{ID: i + 1, Name: name, Markdown: texts[i], Version: versions[i]}
	}
	return out, nil
}

func (r Repository) ReadResumes(ctx context.Context, user string) (Resumes, error) {
	if r.DB == nil || user == "" {
		return Resumes{}, ErrUnavailable
	}
	return readResumes(ctx, r.DB, user)
}

func ResolveResume(ctx context.Context, q profileRowQueryer, user string, id int) (NamedResume, error) {
	list, err := readResumes(ctx, q, user)
	if err != nil {
		return NamedResume{}, err
	}
	if id == 0 {
		id = list.Primary
	}
	if id < 1 || id > len(list.Items) {
		return NamedResume{}, ErrResumeSelection
	}
	return list.Items[id-1], nil
}

func (r Repository) SaveResumes(ctx context.Context, user string, list Resumes) (Resumes, error) {
	if r.DB == nil || user == "" {
		return Resumes{}, ErrUnavailable
	}
	if len(list.Items) < 1 || len(list.Items) > 3 || list.Primary < 1 || list.Primary > len(list.Items) {
		return Resumes{}, ErrResumeSelection
	}
	names := resumeNames{Primary: list.Primary}
	for i, item := range list.Items {
		name := strings.TrimSpace(item.Name)
		if item.ID != i+1 || name == "" || !utf8.ValidString(name) || strings.ContainsRune(name, 0) || utf8.RuneCountInString(name) > 120 || ValidateResume(TextInput{Text: item.Markdown}) != nil {
			return Resumes{}, ErrResumeSelection
		}
		names.Names = append(names.Names, name)
	}
	metadata, err := json.Marshal(names)
	if err != nil {
		return Resumes{}, err
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Resumes{}, err
	}
	defer tx.Rollback()
	// Блокируем настройки и первое резюме, включая первый INSERT.
	for _, key := range []string{ResumesMetadataPrefix + user, user} {
		if _, err = tx.ExecContext(ctx, `INSERT INTO user_profiles(user_name) VALUES(?) ON DUPLICATE KEY UPDATE user_name=VALUES(user_name)`, key); err != nil {
			return Resumes{}, err
		}
	}
	current, err := readResumes(ctx, tx, user)
	if err != nil {
		return Resumes{}, err
	}
	if current.Revision != list.Revision || current.LegacyRevision != list.LegacyRevision || len(list.Items) < len(current.Items) {
		return Resumes{}, ErrConflict
	}
	keys := [...]string{user, Resume2Prefix + user, Resume3Prefix + user}
	for i, item := range list.Items {
		if i < len(current.Items) && item.Markdown == current.Items[i].Markdown {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO user_profiles(user_name,resume_md,revision,updated_at) VALUES(?,?,1,UTC_TIMESTAMP())
			ON DUPLICATE KEY UPDATE resume_md=VALUES(resume_md),revision=revision+1,updated_at=VALUES(updated_at)`, keys[i], item.Markdown); err != nil {
			return Resumes{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE user_profiles SET resume_md=?,revision=revision+1,updated_at=UTC_TIMESTAMP() WHERE user_name=?`, string(metadata), ResumesMetadataPrefix+user); err != nil {
		return Resumes{}, err
	}
	saved, err := readResumes(ctx, tx, user)
	if err != nil {
		return Resumes{}, err
	}
	return saved, tx.Commit()
}
