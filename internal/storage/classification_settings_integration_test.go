//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"itmap/internal/auth"
	"itmap/internal/classification"
	"itmap/internal/httpapi"
)

// Тест запускается только на одноразовой itmap_ci с одноразовым generation.
func TestMariaDBClassificationSettingsDisposable(t *testing.T) {
	requireSettingsCI(t)
	f:=openSettingsSQL(t)
	const rootID, childID, secondID = int64(1904381),int64(1904382),int64(1904383)
	t.Cleanup(func(){
		ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
		defer cancel()
		for _,sql := range []string{
			"DELETE FROM vacancy_role_classification WHERE role_id IN (1904381,1904382,1904383)",
			"DELETE FROM vacancy_roles WHERE id IN (1904382,1904383,1904381)",
		}{
			if _,err:=f.db.ExecContext(ctx,sql);err!=nil {t.Errorf("GO-438 cleanup: %v",err)}
		}
	})
	f.exec(`INSERT INTO vacancy_roles(id,stable_key,name,parent_id,sort_order) VALUES
		(1904381,'root-it-manager','CI Руководитель',NULL,1),
		(1904382,'ci-classification-child','CI Дочерняя',1904381,1),
		(1904383,'ci-classification-custom','CI Пустой',NULL,2)`)
	// Специально пустой ручной промт с revision 7: GET не перезаписывает.
	f.exec("INSERT INTO vacancy_role_classification(role_id,markdown,revision,parsing_enabled) VALUES(1904383,'',7,1)")
	read:=classification.Repository{DB:f.reader}
	write:=classification.Repository{DB:f.db}
	before:=dashboardFingerprint(f)
	overview,err:=read.Read(f.ctx)
	if err!=nil {t.Fatal(err)}
	if len(overview.Roles)!=2 || overview.MaxLength!=10000 {t.Fatalf("unexpected role settings %+v",overview)}
	if overview.Roles[0].RoleID!=rootID || overview.Roles[0].Enabled || overview.Roles[0].Revision!=0 ||
		overview.Roles[0].DefaultMarkdown==nil || !strings.Contains(*overview.Roles[0].DefaultMarkdown,"Product Owner") ||
		len(overview.Roles[0].Descendants)!=1 || overview.Roles[0].Descendants[0].Path!="CI Руководитель → CI Дочерняя" {
		t.Fatalf("root and descendants/default mismatch: %+v",overview.Roles[0])
	}
	if overview.Roles[1].RoleID!=secondID || !overview.Roles[1].Enabled || overview.Roles[1].Markdown!="" ||
		overview.Roles[1].Revision!=7 || overview.Roles[1].DefaultMarkdown!=nil {
		t.Fatalf("existing deliberately empty prompt overwritten in GET: %+v",overview.Roles[1])
	}
	if before!=dashboardFingerprint(f) {t.Fatal("GET wrote to disposable MariaDB")}
	// Отсутствующий root получает первую версию только при явном PUT.
	enabled,err:=write.SaveEnabled(f.ctx,rootID,0,true)
	if err!=nil || enabled.Revision!=1 || !enabled.Enabled || enabled.Markdown!="" {
		t.Fatalf("initial enable failed %+v %v",enabled,err)
	}
	if _,err=write.SaveEnabled(f.ctx,rootID,0,false);!errors.Is(err,classification.ErrConflict) {
		t.Fatal("stale CAS allowed a lost update",err)
	}
	saved,err:=write.SavePrompt(f.ctx,rootID,1,"Работа с командами ✅")
	if err!=nil || saved.Revision!=2 || saved.Markdown!="Работа с командами ✅" || !saved.Enabled {
		t.Fatalf("save lost enabled state %+v %v",saved,err)
	}
	if _,err=write.SavePrompt(f.ctx,rootID,1,"old overwrite");!errors.Is(err,classification.ErrConflict) {
		t.Fatal("stale prompt overwrote new markdown",err)
	}
	if _,err=write.SaveEnabled(f.ctx,childID,0,true);!errors.Is(err,classification.ErrConflict) {
		t.Fatal("child role was editable",err)
	}
	if _,err=write.SavePrompt(f.ctx,9999999,0,"unknown");!errors.Is(err,classification.ErrConflict) {
		t.Fatal("unknown role was editable",err)
	}
	if _,err=write.SavePrompt(f.ctx,rootID,2,strings.Repeat("✅",10001));!errors.Is(err,classification.ErrInput){
		t.Fatal("oversize Unicode prompt accepted",err)
	}
	empty,err:=write.SavePrompt(f.ctx,secondID,7,"")
	if err!=nil || empty.Revision!=8 || !empty.Enabled || empty.Markdown!="" {
		t.Fatalf("empty user prompt was not preserved %+v %v",empty,err)
	}
	// Два параллельных PUT с одной revision: ровно один должен победить.
	var wg sync.WaitGroup
	out:=make(chan error,2)
	for _,v:=range []bool{false,true}{
		wg.Add(1)
		go func(value bool){defer wg.Done();_,e:=write.SaveEnabled(f.ctx,rootID,2,value);out<-e}(v)
	}
	wg.Wait();close(out)
	ok,conflicts:=0,0
	for e:=range out{switch {case e==nil:ok++;case errors.Is(e,classification.ErrConflict):conflicts++;default:t.Fatal("unexpected concurrent update error",e)}}
	if ok!=1||conflicts!=1 {t.Fatalf("CAS race: successes=%d conflicts=%d",ok,conflicts)}
	after,err:=read.Read(f.ctx)
	if err!=nil || len(after.Roles)!=2 || after.Roles[0].Revision!=3 || after.Roles[0].Markdown!="Работа с командами ✅" {
		t.Fatalf("reload failed %+v %v",after,err)
	}
	// Авторизованный Go HTTP с тем же disposable repository, без backend fallback.
	m,err:=auth.NewManager(strings.Repeat("z",64),map[string]string{"eugene":"CI_pw","nastya":"CI_pw"})
	if err!=nil{t.Fatal(err)}
	u,_:=m.Authenticate("eugene","CI_pw")
	token,err:=m.Issue(u,time.Now());if err!=nil{t.Fatal(err)}
	h:=httpapi.NewHandlerWithOptions(nil,httpapi.Options{Auth:m,ClassificationSettings:write})
	req:=httptest.NewRequest("GET","https://example.invalid/api/classification/settings",nil)
	req.AddCookie(&http.Cookie{Name:auth.CookieName,Value:token})
	resp:=httptest.NewRecorder()
	before=dashboardFingerprint(f)
	h.ServeHTTP(resp,req)
	if resp.Code!=200{t.Fatalf("Go settings GET: %d %s",resp.Code,resp.Body.String())}
	var remote classification.SettingsView
	if err:=json.Unmarshal(resp.Body.Bytes(),&remote);err!=nil || len(remote.Roles)!=2 {
		t.Fatalf("Go classification DTO: %v",err)
	}
	if before!=dashboardFingerprint(f){t.Fatal("Go GET mutated MariaDB")}
	// Отрицательный путь не должен раскрывать данные роли не-администратору.
	guest,_:=m.Authenticate("nastya","CI_pw")
	guestToken,err:=m.Issue(guest,time.Now());if err!=nil{t.Fatal(err)}
	req=httptest.NewRequest("GET","https://example.invalid/api/classification/settings",nil)
	req.AddCookie(&http.Cookie{Name:auth.CookieName,Value:guestToken})
	resp=httptest.NewRecorder();h.ServeHTTP(resp,req)
	if resp.Code!=403{t.Fatalf("non-admin got classification config: %d",resp.Code)}
	t.Log("PASS: SELECT-only, root-only, stable defaults, deliberate empty, CAS, concurrent PUT, UTF-8, admin HTTP, no providers")
}
