//go:build mariadb

package vacancies

import (
    "context"
    "database/sql"
    "os"
    "testing"
    "time"

    _ "github.com/go-sql-driver/mysql"
)

// Set ITMAP_TEST_MARIADB_DSN to a READ-ONLY account on an isolated staging
// database restored from the private source dump. No private data are logged.
func TestMariaDBVacancyListReadOnlyIntegration(t *testing.T) {
    dsn:=os.Getenv("ITMAP_TEST_MARIADB_DSN")
    if dsn==""{t.Skip("set ITMAP_TEST_MARIADB_DSN for isolated staging integration")}
    db,err:=sql.Open("mysql",dsn)
    if err!=nil{t.Fatal("unable to initialize integration driver")}
    defer db.Close()
    ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
    defer cancel()
    p:=All
    for _,user:=range []string{"Eugene","Demo"}{
        result,err:= (Repository{DB:db}).List(ctx,user,&p,time.Now())
        if err!=nil{t.Fatalf("list failed for %s: %T",user,err)}
        if result.Vacancies==nil || result.RoleTree==nil || result.Entities==nil || result.ApplicationSummary==nil{t.Fatalf("unexpected nil arrays for %s",user)}
    }
}
