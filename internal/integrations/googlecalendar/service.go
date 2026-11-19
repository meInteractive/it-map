package googlecalendar

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"itmap/internal/legacycrypto"
)

var (
	ErrStorage      = errors.New("google calendar storage unavailable")
	ErrLinkedTask   = errors.New("google calendar existing task linkage needs reconciliation")
	ErrAccount      = errors.New("google calendar identity unavailable")
	ErrDisconnected = errors.New("google calendar not connected")
)

type Service struct {
	DB     *sql.DB
	Secret string
	OAuth  Config
	Tokens TokenClient
}

type Status struct {
	Configured  bool   `json:"configured"`
	Connected   bool   `json:"connected"`
	AccountEmail string `json:"accountEmail"`
	SyncedTasks int64  `json:"syncedTasks"`
	SyncErrors  int64  `json:"syncErrors"`
}

type SyncResult struct {
	Synced int `json:"synced"`
	Errors int `json:"errors"`
	Skipped int `json:"skipped"`
}

type connection struct {
	email string
	encrypted string
	calendarID string
}

func (s Service) codec() (*legacycrypto.Codec, error) {
	return legacycrypto.New(s.Secret, legacycrypto.Calendar)
}

func (s Service) readConnection(ctx context.Context, user string) (connection, error) {
	var c connection
	if s.DB == nil || !validUser(user) {
		return c, ErrStorage
	}
	err := s.DB.QueryRowContext(ctx,
		"SELECT account_email,refresh_token_encrypted,calendar_id FROM google_calendar_user_connection WHERE user_name=?", user).
		Scan(&c.email, &c.encrypted, &c.calendarID)
	if errors.Is(err,sql.ErrNoRows) {
		return connection{},ErrDisconnected
	}
	if err != nil {
		return connection{},ErrStorage
	}
	return c,nil
}

func (s Service) Status(ctx context.Context, user string) (Status, error) {
	out := Status{Configured:s.OAuth.Valid(),AccountEmail:""}
	if s.DB == nil || !validUser(user) {
		return out,ErrStorage
	}
	c,e := s.readConnection(ctx,user)
	if e != nil && !errors.Is(e,ErrDisconnected) {
		return out,e
	}
	if e == nil {
		out.Connected=true
		out.AccountEmail=c.email
	}
	e = s.DB.QueryRowContext(ctx,`SELECT
	COALESCE(SUM(CASE WHEN google_event_id IS NOT NULL AND google_sync_status='synced' THEN 1 ELSE 0 END),0),
	COALESCE(SUM(CASE WHEN google_sync_status='error' THEN 1 ELSE 0 END),0)
	FROM company_tasks WHERE user_name=?`,user).Scan(&out.SyncedTasks,&out.SyncErrors)
	if e != nil {
		return Status{},ErrStorage
	}
	return out,nil
}

func (s Service) accountEmail(ctx context.Context, accessToken string) (string,error) {
	if accessToken=="" || len(accessToken)>8192 {
		return "",ErrAccount
	}
	client:=s.Tokens.HTTP
	if client==nil { client=&http.Client{Timeout:12*time.Second} }
	copyClient:=*client
	copyClient.CheckRedirect=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
	ctx,cancel:=context.WithTimeout(ctx,12*time.Second)
	defer cancel()
	req,e:=http.NewRequestWithContext(ctx,http.MethodGet,"https://www.googleapis.com/oauth2/v2/userinfo",nil)
	if e!=nil {return "",ErrAccount}
	req.Header.Set("Authorization","Bearer "+accessToken)
	resp,e:=copyClient.Do(req)
	if e!=nil {return "",ErrAccount}
	defer resp.Body.Close()
	if resp.StatusCode!=200 {return "",ErrAccount}
	var data struct {
		Email string `json:"email"`
		VerifiedEmail bool `json:"verified_email"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body,4096)).Decode(&data)!=nil ||
		!data.VerifiedEmail || len(data.Email)>255 || !strings.Contains(data.Email,"@") {
		return "",ErrAccount
	}
	return data.Email,nil
}

// verifyPriorLinks проверяет только уже привязанные события. При удалённом
// локальном OAuth подключении старый email неизвестен, поэтому реальную
// принадлежность события доказывает GET по детерминированному ID в новом
// аккаунте и private provenance. Слишком большие/сомнительные группы
// требуют операторского восстановления, а не автоматического перепривязывания.
func (s Service) verifyPriorLinks(ctx context.Context, user, accessToken string) (int64,error) {
	rows,e:=s.DB.QueryContext(ctx,"SELECT id,google_event_id FROM company_tasks WHERE user_name=? AND google_event_id IS NOT NULL ORDER BY id LIMIT 101",user)
	if e!=nil{return 0,ErrStorage}
	type link struct{id int64; event string}
	var list []link
	for rows.Next() {
		var l link
		if rows.Scan(&l.id,&l.event)!=nil {rows.Close();return 0,ErrStorage}
		list=append(list,l)
	}
	e=rows.Err()
	rows.Close()
	if e!=nil{return 0,ErrStorage}
	if len(list)>100{return 0,ErrLinkedTask}
	for _,l:=range list{
		expected:=EventID(user,l.id)
		if l.event!=expected{return 0,ErrLinkedTask}
		remote,_,err:=s.Tokens.eventCall(ctx,accessToken,"primary",http.MethodGet,expected,nil)
		if err!=nil||!matchesEvent(remote,Event{ID:expected}) {return 0,ErrLinkedTask}
	}
	return int64(len(list)),nil
}

// Connect сохраняет refresh token лишь после успешного OAuth exchange.
// Переподключение к другому Google account при старых task links запрещено.
func (s Service) Connect(ctx context.Context, user,code,state string) error {
	if s.DB==nil || !s.OAuth.Valid() || !validUser(user) {return ErrConfiguration}
	token,e:=s.Tokens.Exchange(ctx,s.OAuth,code,state)
	if e!=nil{return e}
	email,e:=s.accountEmail(ctx,token.AccessToken)
	if e!=nil{return e}
	// После отключения локальный token исчезает, но ID созданных событий
	// остаются. Проверим их в выбранном Google account ДО SQL-транзакции.
	// Это позволяет безопасно переподключить тот же аккаунт и не объединять
	// чужие календари. Ни одной внешней операции записи здесь нет.
	verified, e := s.verifyPriorLinks(ctx, user, token.AccessToken)
	if e != nil {return e}
	codec,e:=s.codec()
	if e!=nil{return ErrConfiguration}
	encrypted,e:=codec.Encrypt(token.RefreshToken)
	if e!=nil{return ErrStorage}
	tx,e:=s.DB.BeginTx(ctx,&sql.TxOptions{Isolation:sql.LevelReadCommitted})
	if e!=nil{return ErrStorage}
	defer tx.Rollback()
	var previous string
	e=tx.QueryRowContext(ctx,"SELECT account_email FROM google_calendar_user_connection WHERE user_name=? FOR UPDATE",user).Scan(&previous)
	if e!=nil&&!errors.Is(e,sql.ErrNoRows){return ErrStorage}
	// Даже если прежнее подключение уже удалено, связанные Event ID нельзя
	// автоматически переносить в другой аккаунт Google.
	var linked int64
	if tx.QueryRowContext(ctx,"SELECT COUNT(*) FROM company_tasks WHERE user_name=? AND google_event_id IS NOT NULL",user).Scan(&linked)!=nil{return ErrStorage}
	if linked != verified || (linked>0 && previous!="" && previous!=email){return ErrLinkedTask}
	_,e=tx.ExecContext(ctx,`INSERT INTO google_calendar_user_connection(user_name,account_email,refresh_token_encrypted,calendar_id,connected_at,updated_at)
	VALUES(?,?,?,'primary',UTC_TIMESTAMP(),UTC_TIMESTAMP())
	ON DUPLICATE KEY UPDATE account_email=VALUES(account_email),refresh_token_encrypted=VALUES(refresh_token_encrypted),calendar_id=VALUES(calendar_id),updated_at=UTC_TIMESTAMP()`,user,email,encrypted)
	if e!=nil{return ErrStorage}
	if tx.Commit()!=nil{return ErrStorage}
	return nil
}

// Disconnect удаляет только локальный refresh token. Существующие события
// Google не стираются и task link metadata сохраняется для сверки.
func (s Service) Disconnect(ctx context.Context,user string) error {
	if s.DB==nil||!validUser(user){return ErrStorage}
	_,e:=s.DB.ExecContext(ctx,"DELETE FROM google_calendar_user_connection WHERE user_name=?",user)
	if e!=nil{return ErrStorage}
	return nil
}

func (s Service) access(ctx context.Context,c connection) (string,error) {
	codec,e:=s.codec()
	if e!=nil{return "",ErrConfiguration}
	refresh,e:=codec.Decrypt(c.encrypted)
	if e!=nil{return "",ErrStorage}
	tok,e:=s.Tokens.Refresh(ctx,s.OAuth,refresh)
	if e!=nil{return "",e}
	return tok.AccessToken,nil
}

type candidateTask struct {
	id int64
	company string
	kind string
	note string
	due string
	event sql.NullString
	status string
}

// Sync — строго ручной и ограниченный проход. Не создаёт фоновых заданий.
// Claim по одному user/task сохраняется в существующем статусе; после падения
// разрешено повторить лишь тот же детерминированный внешний Event ID.
func (s Service) Sync(ctx context.Context,user string) (SyncResult,error) {
	var out SyncResult
	if !s.OAuth.Valid(){return out,ErrConfiguration}
	c,e:=s.readConnection(ctx,user)
	if e!=nil{return out,e}
	access,e:=s.access(ctx,c)
	if e!=nil{return out,e}
	rows,e:=s.DB.QueryContext(ctx,`SELECT id,company_name,type,note,due_at,google_event_id,google_sync_status
	FROM company_tasks WHERE user_name=? AND completed=0
	AND (google_event_id IS NULL OR google_sync_status<>'synced')
	ORDER BY due_at,id LIMIT 100`,user)
	if e!=nil{return out,ErrStorage}
	var tasks []candidateTask
	for rows.Next(){
		var t candidateTask
		if rows.Scan(&t.id,&t.company,&t.kind,&t.note,&t.due,&t.event,&t.status)!=nil{
			rows.Close();return out,ErrStorage
		}
		tasks=append(tasks,t)
	}
	e=rows.Err()
	rows.Close()
	if e!=nil{return out,ErrStorage}
	now:=time.Now().UTC()
	for _,t:=range tasks{
		if ctx.Err()!=nil{return out,ctx.Err()}
		if strings.HasPrefix(t.status,"inflight:"){
			ts,err:=time.Parse(time.RFC3339,strings.TrimPrefix(t.status,"inflight:"))
			if err!=nil||now.Sub(ts)<3*time.Minute{
				out.Skipped++;continue
			}
		}
		claim:="inflight:"+now.Format(time.RFC3339)
		result,e:=s.DB.ExecContext(ctx,`UPDATE company_tasks SET google_sync_status=?
		WHERE user_name=? AND id=? AND completed=0 AND google_sync_status=?`,claim,user,t.id,t.status)
		if e!=nil{return out,ErrStorage}
		if n,_:=result.RowsAffected();n!=1{
			out.Skipped++;continue
		}
		expected:=EventID(user,t.id)
		if t.event.Valid && t.event.String!=expected {
			// Старые event ID нельзя заменять новой идентичностью.
			_,_=s.DB.ExecContext(ctx,`UPDATE company_tasks SET google_sync_status='error',google_sync_error='legacy_event_link' WHERE user_name=? AND id=? AND google_sync_status=?`,user,t.id,claim)
			out.Errors++;continue
		}
		wanted,e:= (EventInput{ID:t.id,User:user,Company:t.company,Type:t.kind,Note:t.note,DueAt:t.due}).Build()
		if e!=nil{
			_,_=s.DB.ExecContext(ctx,`UPDATE company_tasks SET google_sync_status='error',google_sync_error='invalid_task' WHERE user_name=? AND id=? AND google_sync_status=?`,user,t.id,claim)
			out.Errors++;continue
		}
		event,e:=s.Tokens.Upsert(ctx,access,c.calendarID,wanted)
		if e!=nil{
			_,_=s.DB.ExecContext(ctx,`UPDATE company_tasks SET google_sync_status='error',google_sync_error='google_event_unknown' WHERE user_name=? AND id=? AND google_sync_status=?`,user,t.id,claim)
			out.Errors++;continue
		}
		res,e:=s.DB.ExecContext(ctx,`UPDATE company_tasks SET google_event_id=?,google_event_html_link=?,google_sync_status='synced',google_sync_error=''
		WHERE user_name=? AND id=? AND google_sync_status=? AND due_at=? AND completed=0`,event.ID,SafeEventLink(event.HTMLLink),user,t.id,claim,t.due)
		if e!=nil{return out,ErrStorage}
		if n,_:=res.RowsAffected();n!=1{
			out.Errors++;continue
		}
		out.Synced++
	}
	return out,nil
}
