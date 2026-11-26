package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"

	"itmap/internal/contacts"
)

func importContactRows(ctx context.Context, tx *sql.Tx, rows []ImportRow, mode string, companies, agencies *importOwners, report *importReport) error {
	for index, row := range rows {
		valid, err := importContact(ctx, tx, row, mode, companies, agencies, report)
		if err != nil {
			return err
		}
		if !valid {
			report.issue("Контакты", row, index, fmt.Errorf("не найдена организация или указаны некорректные данные контакта"))
			report.counts["contactsSkipped"]++
		}
	}
	return nil
}

func importContact(ctx context.Context, tx *sql.Tx, row ImportRow, mode string, companies, agencies *importOwners, report *importReport) (bool, error) {
	owners := companies
	switch importKey(importText(row, "entityType")) {
	case "агентство", "agency":
		owners = agencies
	case "компания", "company":
	default:
		return false, nil
	}
	id, err := importID(row, "entityId")
	if err != nil {
		return false, nil
	}
	name := importText(row, "entityName")
	idx, found, err := owners.lookup(id, name, mode == "create")
	if err != nil || !found || (name != "" && importKey(name) != importKey(nameOf(owners.entities[idx]))) {
		return false, nil
	}
	owner := nameOf(owners.entities[idx])
	pos, err := importNumber(importText(row, "position"), 1, 3, true)
	if err != nil {
		return false, nil
	}

	// Блокировка той же CRM-строки, что и в обычной форме редактирования контакта.
	var crmName string
	err = tx.QueryRowContext(ctx, "SELECT company_name FROM company_crm WHERE company_name=? FOR UPDATE", owner).Scan(&crmName)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	var current contacts.NormalizedInput
	var contactID int64
	err = tx.QueryRowContext(ctx, `SELECT id,full_name,linkedin_url,facebook_url,setka_url,telegram,phone,email,specialization
		FROM hiring_contacts WHERE company_name=? AND position=? FOR UPDATE`, owner, int(pos)).
		Scan(&contactID, &current.FullName, &current.LinkedinURL, &current.FacebookURL, &current.SetkaURL, &current.Telegram, &current.Phone, &current.Email, &current.Specialization)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if contactID > 0 && mode == "create" {
		report.counts["contactsSkipped"]++
		return true, nil
	}
	fields := maps.Clone(row)
	if contactID > 0 {
		for key, value := range map[string]string{"fullName": current.FullName, "linkedinUrl": current.LinkedinURL, "facebookUrl": current.FacebookURL, "setkaUrl": current.SetkaURL, "telegram": current.Telegram, "phone": current.Phone, "email": current.Email, "specialization": current.Specialization} {
			if importText(row, key) == "" {
				fields[key], _ = json.Marshal(value)
			}
		}
	}
	normalized, err := contacts.NormalizeInput(contacts.Input(fields), owner, nil)
	if err != nil {
		return false, nil
	}
	if err = ensureImportCRM(ctx, tx, owner); err != nil {
		return false, err
	}
	args := []any{normalized.FullName, normalized.LinkedinURL, normalized.FacebookURL, normalized.SetkaURL, normalized.Telegram, normalized.Phone, normalized.Email, normalized.Specialization}
	if contactID > 0 {
		_, err = tx.ExecContext(ctx, `UPDATE hiring_contacts SET full_name=?,linkedin_url=?,facebook_url=?,setka_url=?,telegram=?,phone=?,email=?,specialization=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, append(args, contactID)...)
		if err != nil {
			return false, err
		}
		report.counts["contactsUpdated"]++
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO hiring_contacts(full_name,linkedin_url,facebook_url,setka_url,telegram,phone,email,specialization,company_name,position) VALUES(?,?,?,?,?,?,?,?,?,?)`, append(args, owner, normalized.Position)...)
		if err != nil {
			return false, err
		}
		report.counts["contactsAdded"]++
	}
	return true, nil
}
