//go:build mariadb && crmwrites

package storage

import (
	"database/sql"
	"itmap/internal/crm"
)

var errCRMFlagFault = errMutationFault

func (f *crmFlagFixture) faultRepo(prefix string) (crm.Repository, *mutationFault, *sql.DB) {
	f.t.Helper()
	fault, db := openMutationFaultPool(f.t, prefix)
	return crm.Repository{DB: db}, fault, db
}
