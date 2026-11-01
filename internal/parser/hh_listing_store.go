package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"itmap/internal/apifysettings"
)

type hhListingPlan struct {
	Cursor          hhListingCursor
	ID, CompanyName string
	ActiveRoles     int
}

func (e HHListingExecutor) hhScope(ctx context.Context, tx *sql.Tx, job Job, v apifysettings.OperationVersion) (string, string, []string, error) {
	if e.Owner == "" || job.Type != "scan_source" || job.SourceID == nil || *job.SourceID != "hh" || job.CompanyID == nil || job.AgencyID != nil || job.CandidateID != nil || v.CompanyID != *job.CompanyID || v.Generation != job.Generation {
		return "", "", nil, ErrJobInput
	}
	if _, err := e.Settings.ResolveOperationTx(ctx, tx, v); err != nil {
		return "", "", nil, err
	}
	row, err := readSettingsRow(ctx, tx, true)
	if err != nil {
		return "", "", nil, ErrSettingsStorage
	}
	view, err := settingsView(ctx, tx, row)
	if err != nil {
		return "", "", nil, ErrSettingsStorage
	}
	var settings Settings
	if json.Unmarshal(view.PublicJSON(), &settings) != nil || !settings.Enabled || settings.OwnerName != e.Owner {
		return "", "", nil, ErrSettingsChanged
	}
	selected := false
	for _, id := range settings.SourceIDs {
		selected = selected || id == "hh"
	}
	if !selected {
		return "", "", nil, ErrSettingsChanged
	}
	var name string
	if tx.QueryRowContext(ctx, "SELECT company_name FROM company_identity WHERE id=? FOR UPDATE", job.CompanyID).Scan(&name) != nil {
		return "", "", nil, ErrSettingsStorage
	}
	if row.favorite != 0 {
		var favorite int
		err = tx.QueryRowContext(ctx, "SELECT favorite FROM company_user_state WHERE company_name=? AND user_name=? FOR UPDATE", name, e.Owner).Scan(&favorite)
		if errors.Is(err, sql.ErrNoRows) || err == nil && favorite != 1 {
			return "", "", nil, ErrHHListingChanged
		}
		if err != nil {
			return "", "", nil, ErrSettingsStorage
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT r.stable_key,c.markdown FROM vacancy_roles r JOIN vacancy_role_classification c ON c.role_id=r.id WHERE r.parent_id IS NULL AND c.parsing_enabled=1 ORDER BY r.sort_order,r.id FOR UPDATE")
	if err != nil {
		return "", "", nil, ErrSettingsStorage
	}
	keys := []string{}
	for rows.Next() {
		var key sql.NullString
		var markdown string
		if err = rows.Scan(&key, &markdown); err != nil {
			break
		}
		if jsStageTrim(markdown) != "" {
			keys = append(keys, key.String)
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return "", "", nil, ErrSettingsStorage
	}
	return name, stageHash(string(view.PublicJSON())), keys, nil
}
func readHHListingCursor(ctx context.Context, tx *sql.Tx, id, owner string, job Job) (hhListingCursor, bool, error) {
	var cur hhListingCursor
	var user, generation, source, url string
	var company, agency sql.NullInt64
	var sourceRev, settingsRev int64
	var raw []byte
	err := tx.QueryRowContext(ctx, "SELECT user_name,generation,company_id,agency_id,source_id,source_url,source_revision,settings_revision,result_json FROM vacancy_discovery_runs WHERE id=? FOR UPDATE", id).Scan(&user, &generation, &company, &agency, &source, &url, &sourceRev, &settingsRev, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return cur, false, nil
	}
	if err != nil {
		return cur, false, ErrSettingsStorage
	}
	if json.Unmarshal(raw, &cur) != nil || cur.Version != 1 || cur.Revision < 1 || user != owner || generation != job.Generation || !company.Valid || company.Int64 != *job.CompanyID || agency.Valid || source != "hh" || cur.Operation.SourceURL != url || cur.Operation.SourceRevision != sourceRev || cur.Operation.SettingsRevision != settingsRev {
		return cur, false, ErrHHListingChanged
	}
	return cur, true, nil
}
func persistHHListingCursor(ctx context.Context, tx *sql.Tx, id, owner string, job Job, c hhListingCursor, created bool) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return ErrSettingsStorage
	}
	now := time.Now().UnixMilli()
	if created {
		_, err = tx.ExecContext(ctx, "INSERT INTO vacancy_discovery_runs(id,user_name,generation,company_id,source_id,source_url,source_revision,settings_revision,result_json,created_at,expires_at) VALUES(?,?,?,?,'hh',?,?,?,?,?,?)", id, owner, job.Generation, job.CompanyID, c.Operation.SourceURL, c.Operation.SourceRevision, c.Operation.SettingsRevision, string(raw), now, now+3600000)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE vacancy_discovery_runs SET result_json=?,expires_at=? WHERE id=?", string(raw), now+3600000, id)
	}
	if err != nil {
		return ErrSettingsStorage
	}
	return nil
}
func (e HHListingExecutor) prepareHHListing(ctx context.Context, tx *sql.Tx, job Job, v apifysettings.OperationVersion) (hhListingPlan, error) {
	plan := hhListingPlan{ID: "go399:" + job.ID}
	cur, exists, err := readHHListingCursor(ctx, tx, plan.ID, e.Owner, job)
	if err != nil {
		return plan, err
	}
	if exists {
		v.Profile = cur.Profile
	}
	name, hash, keys, err := e.hhScope(ctx, tx, job, v)
	if err != nil {
		return plan, err
	}
	if exists {
		if !reflect.DeepEqual(cur.Operation, v) || cur.SchedulerHash != hash {
			return plan, ErrHHListingChanged
		}
	} else {
		cur = hhListingCursor{Version: 1, Revision: 1, Operation: v, SchedulerHash: hash, Profile: v.Profile, TitleQuery: hhTitleQuery(keys)}
		if err = persistHHListingCursor(ctx, tx, plan.ID, e.Owner, job, cur, true); err != nil {
			return plan, err
		}
	}
	plan.Cursor, plan.CompanyName, plan.ActiveRoles = cur, name, len(keys)
	return plan, nil
}

var errHHIdentity = errors.New("идентификаторы или владелец кандидата конфликтуют")

func saveHHListingCandidate(ctx context.Context, tx *sql.Tx, job Job, name, sourceURL string, item hhListingVacancy) (int64, bool, bool, error) {
	namespace := fmt.Sprintf("company:%d:hh:https://hh.ru", *job.CompanyID)
	external, err := canonicalHash([]string{namespace, item.ExternalID})
	if err != nil {
		return 0, false, false, err
	}
	keys := []string{"external:" + external, "url:" + stageHash(item.URL)}
	rows, err := tx.QueryContext(ctx, "SELECT candidate_id FROM vacancy_candidate_identities WHERE identity_key IN (?,?) FOR UPDATE", keys[0], keys[1])
	if err != nil {
		return 0, false, false, ErrSettingsStorage
	}
	var id int64
	for rows.Next() {
		var found int64
		if err = rows.Scan(&found); err != nil {
			break
		}
		if id != 0 && id != found {
			err = errHHIdentity
			break
		}
		id = found
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return 0, false, false, err
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	hash := stageHash(item.Title)
	created := id == 0
	imported := false
	if created {
		token, err := stageToken()
		if err != nil {
			return 0, false, false, err
		}
		result, err := tx.ExecContext(ctx, "INSERT INTO vacancy_candidates(creation_token,company_id,company_name_raw,title_raw,title_hash,vacancy_url,canonical_url,source_url,source_id,source_namespace,external_job_id,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,'hh',?,?,'site',?,?,?,?)", token, job.CompanyID, name, item.Title, hash, item.URL, item.URL, sourceURL, namespace, item.ExternalID, now, now, now, now)
		if err != nil {
			return 0, false, false, ErrSettingsStorage
		}
		id, err = result.LastInsertId()
		if err != nil {
			return 0, false, false, ErrSettingsStorage
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO parser_candidate_intakes(candidate_token,parser,source_id,received_at) VALUES(?,'apify','hh',?)", token, now); err != nil {
			return 0, false, false, ErrSettingsStorage
		}
	} else {
		var company, agency, main sql.NullInt64
		var oldNamespace, oldExternal sql.NullString
		if err = tx.QueryRowContext(ctx, "SELECT company_id,agency_id,main_vacancy_id,source_namespace,external_job_id FROM vacancy_candidates WHERE id=? FOR UPDATE", id).Scan(&company, &agency, &main, &oldNamespace, &oldExternal); err != nil {
			return 0, false, false, ErrSettingsStorage
		}
		if agency.Valid || company.Valid && company.Int64 != *job.CompanyID || oldNamespace.Valid && oldNamespace.String == namespace && oldExternal.Valid && oldExternal.String != item.ExternalID {
			return 0, false, false, errHHIdentity
		}
		imported = main.Valid
		_, err = tx.ExecContext(ctx, "UPDATE vacancy_candidates SET title_updated_at=CASE WHEN title_raw<>? THEN ? ELSE title_updated_at END,title_revision=title_revision+CASE WHEN title_hash<>? THEN 1 ELSE 0 END,title_raw=?,title_hash=?,company_id=?,company_name_raw=?,vacancy_url=?,canonical_url=?,source_url=?,source_id='hh',source_namespace=?,external_job_id=?,acquisition_method='site',last_seen_at=GREATEST(last_seen_at,?),updated_at=? WHERE id=?", item.Title, now, hash, item.Title, hash, job.CompanyID, name, item.URL, item.URL, sourceURL, namespace, item.ExternalID, now, now, id)
		if err != nil {
			return 0, false, false, ErrSettingsStorage
		}
	}
	for _, key := range keys {
		if _, err = tx.ExecContext(ctx, "INSERT INTO vacancy_candidate_identities(identity_key,candidate_id) VALUES(?,?) ON DUPLICATE KEY UPDATE candidate_id=candidate_id", key, id); err != nil {
			return 0, false, false, ErrSettingsStorage
		}
	}
	for _, key := range keys {
		var target int64
		if tx.QueryRowContext(ctx, "SELECT candidate_id FROM vacancy_candidate_identities WHERE identity_key=? FOR UPDATE", key).Scan(&target) != nil || target != id {
			return 0, false, false, ErrSettingsStorage
		}
	}
	return id, created, imported, nil
}
