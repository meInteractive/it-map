package coverletters

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"itmap/internal/profiles"
)

// Input — согласованный SQL-снимок всех зависимостей генерации. В него входят
// поколения restore и revision модели/ключа, но не входят тексты в логах.
type Input struct {
	Generation string
	VacancyID int64
	Title string
	Description string
	SharedSubject string
	SharedMD string
	VacancyCreated string
	VacancyUpdated string
	Resume string
	ResumeRevision int64
	Prompt string
	PromptRevision int64
	Config profiles.ModelInput
	OpenAIKeyRevision int64
	PersonalSubject string
	PersonalMD string
	PersonalOverride bool
	PersonalRevision int64
	HasPersonal bool
}

func (in Input) Effective() (Letter,bool) {
	if in.HasPersonal && in.PersonalOverride {
		return Letter{in.PersonalSubject,in.PersonalMD,"personal"},true
	}
	if strings.TrimSpace(in.SharedMD)!="" {
		return Letter{in.SharedSubject,in.SharedMD,"vacancy"},true
	}
	if in.HasPersonal {
		return Letter{in.PersonalSubject,in.PersonalMD,"personal"},true
	}
	return Letter{},false
}

func (in Input) Hash() string {
	b,_:=json.Marshal(in)
	d:=sha256.Sum256(b)
	return hex.EncodeToString(d[:])
}

type Repository struct{DB *sql.DB}

type queryRow interface {QueryRowContext(context.Context,string,...any)*sql.Row}

// SQL всегда выбирает только последнюю персональную запись и не умножает
// вакансии из-за исторических дубликатов без уникального ключа в MariaDB.
const inputSQL = `SELECT g.generation,v.title,COALESCE(v.description_md,''),
 COALESCE(v.cover_letter_subject,''),COALESCE(v.cover_letter_md,''),
 COALESCE(v.created_at,''),COALESCE(v.updated_at,''),
 COALESCE(p.resume_md,''),COALESCE(p.revision,0),
 COALESCE(sp.markdown,''),COALESCE(sp.revision,0),
 COALESCE(pm.provider,'openrouter'),COALESCE(pm.openai_model,''),
 COALESCE(pm.openrouter_model,'best_free'),COALESCE(pm.reasoning_effort,'medium'),
 COALESCE(pm.revision,0),COALESCE(k.revision,0),
 personal.subject,personal.markdown,personal.override_shared,personal.revision
 FROM company_vacancies v
 CROSS JOIN app_runtime_generation g
 LEFT JOIN user_profiles p ON p.user_name=?
 LEFT JOIN system_prompts sp ON sp.kind='cover_letter'
 LEFT JOIN prompt_model_settings pm ON pm.kind='cover_letter'
 LEFT JOIN openai_settings k ON k.id=1
 LEFT JOIN vacancy_cover_letters personal ON personal.user_name=? AND personal.vacancy_id=v.id
 WHERE g.id=1 AND v.id=?
 ORDER BY personal.revision DESC,personal.created_at DESC LIMIT 1`

func readInput(ctx context.Context, q queryRow, user string, id int64) (Input,error) {
	var in Input
	in.VacancyID=id
	var subject,body sql.NullString
	var override,revision sql.NullInt64
	err:=q.QueryRowContext(ctx,inputSQL,user,user,id).Scan(
		&in.Generation,&in.Title,&in.Description,&in.SharedSubject,&in.SharedMD,
		&in.VacancyCreated,&in.VacancyUpdated,&in.Resume,&in.ResumeRevision,
		&in.Prompt,&in.PromptRevision,&in.Config.Provider,&in.Config.OpenAIModel,
		&in.Config.OpenRouterModel,&in.Config.ReasoningEffort,&in.Config.Revision,
		&in.OpenAIKeyRevision,&subject,&body,&override,&revision,
	)
	if errors.Is(err,sql.ErrNoRows){return Input{},ErrMissing}
	if err!=nil{return Input{},ErrUnavailable}
	in.HasPersonal=subject.Valid && body.Valid
	if in.HasPersonal{
		in.PersonalSubject=subject.String
		in.PersonalMD=body.String
		in.PersonalOverride=override.Int64!=0
		in.PersonalRevision=revision.Int64
	}
	return in,nil
}

func (r Repository) Read(ctx context.Context,user string,id int64)(Input,error){
	if r.DB==nil||user==""||id<=0{return Input{},ErrUnavailable}
	return readInput(ctx,r.DB,user,id)
}

func newToken()(string,error){
	var b [16]byte
	if _,e:=rand.Read(b[:]);e!=nil{return "",ErrUnavailable}
	b[6]=b[6]&0x0f|0x40
	b[8]=b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x",b[:4],b[4:6],b[6:8],b[8:10],b[10:]),nil
}

type Lease struct {
	User string
	VacancyID int64
	Generation string
	InputHash string
	Token string
}

const leaseDuration=90*time.Second

func (r Repository) Claim(ctx context.Context, user string, in Input, regenerate bool)(Lease,error){
	if r.DB==nil||user==""||in.VacancyID<=0{return Lease{},ErrUnavailable}
	token,e:=newToken();if e!=nil{return Lease{},e}
	tx,e:=r.DB.BeginTx(ctx,&sql.TxOptions{Isolation:sql.LevelReadCommitted})
	if e!=nil{return Lease{},ErrUnavailable};defer tx.Rollback()
	// Сериализация по вакансии защищает и первый INSERT lease, и ручные PUT.
	var exists int64
	if e=tx.QueryRowContext(ctx,"SELECT id FROM company_vacancies WHERE id=? FOR UPDATE",in.VacancyID).Scan(&exists);e!=nil{
		if errors.Is(e,sql.ErrNoRows){return Lease{},ErrMissing}
		return Lease{},ErrUnavailable
	}
	current,e:=readInput(ctx,tx,user,in.VacancyID)
	if e!=nil{return Lease{},e}
	if current.Hash()!=in.Hash(){return Lease{},ErrChanged}
	if old,has:=current.Effective();has&&!regenerate&&strings.TrimSpace(old.CoverLetterMD)!=""{return Lease{},ErrChanged}
	var expiry int64
	var originalToken string
	e=tx.QueryRowContext(ctx,"SELECT expires_at,owner_token FROM cover_letter_generation_leases WHERE user_name=? AND vacancy_id=? FOR UPDATE",user,in.VacancyID).Scan(&expiry,&originalToken)
	now:=time.Now().UnixMilli()
	switch{
	case errors.Is(e,sql.ErrNoRows):
		_,e=tx.ExecContext(ctx,`INSERT INTO cover_letter_generation_leases
		  (user_name,vacancy_id,generation,input_hash,owner_token,expires_at) VALUES(?,?,?,?,?,?)`,
			user,in.VacancyID,in.Generation,in.Hash(),token,now+leaseDuration.Milliseconds())
	case e!=nil:
		return Lease{},ErrUnavailable
	case expiry>now:
		return Lease{},ErrBusy
	default:
		_,e=tx.ExecContext(ctx,`UPDATE cover_letter_generation_leases
		  SET generation=?,input_hash=?,owner_token=?,expires_at=?
		  WHERE user_name=? AND vacancy_id=? AND owner_token=?`,
			in.Generation,in.Hash(),token,now+leaseDuration.Milliseconds(),user,in.VacancyID,originalToken)
	}
	if e!=nil{return Lease{},ErrUnavailable}
	if e=tx.Commit();e!=nil{return Lease{},ErrUnavailable}
	return Lease{user,in.VacancyID,in.Generation,in.Hash(),token},nil
}

func (r Repository) Check(ctx context.Context,l Lease)error{
	if r.DB==nil{return ErrUnavailable}
	var generation,hash,token string
	var expiry int64
	e:=r.DB.QueryRowContext(ctx,`SELECT l.generation,l.input_hash,l.owner_token,l.expires_at
	  FROM cover_letter_generation_leases l WHERE l.user_name=? AND l.vacancy_id=?`,l.User,l.VacancyID).
		Scan(&generation,&hash,&token,&expiry)
	if e!=nil||generation!=l.Generation||hash!=l.InputHash||token!=l.Token||expiry<=time.Now().UnixMilli(){
		return ErrChanged
	}
	input,e:=r.Read(ctx,l.User,l.VacancyID)
	if e!=nil||input.Generation!=l.Generation||input.Hash()!=l.InputHash{return ErrChanged}
	return nil
}

func (r Repository) Renew(ctx context.Context,l Lease)error{
	if e:=r.Check(ctx,l);e!=nil{return e}
	result,e:=r.DB.ExecContext(ctx,`UPDATE cover_letter_generation_leases
	  SET expires_at=? WHERE user_name=? AND vacancy_id=? AND generation=?
	  AND input_hash=? AND owner_token=? AND expires_at>?`,
		time.Now().Add(leaseDuration).UnixMilli(),l.User,l.VacancyID,l.Generation,l.InputHash,l.Token,time.Now().UnixMilli())
	if e!=nil{return ErrUnavailable}
	n,e:=result.RowsAffected()
	if e!=nil||n!=1{return ErrChanged}
	return nil
}

func (r Repository) Release(l Lease){
	if r.DB==nil{return}
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
	_,_=r.DB.ExecContext(ctx,`DELETE FROM cover_letter_generation_leases WHERE
	  user_name=? AND vacancy_id=? AND generation=? AND input_hash=? AND owner_token=?`,
		l.User,l.VacancyID,l.Generation,l.InputHash,l.Token)
}

// Save проверяет поколение restore и все входы внутри одной write-транзакции,
// сохраняя личную строку (не общую company_vacancies) только после успеха модели.
func (r Repository) Save(ctx context.Context,l Lease,in Input,answer Parsed,regenerate bool)(Letter,error){
	if r.DB==nil||answer.Subject==""||answer.Markdown==""{return Letter{},ErrInvalid}
	if len([]rune(answer.Subject))>MaxSubject||len([]rune(answer.Markdown))>MaxBody{return Letter{},ErrInvalid}
	tx,err:=r.DB.BeginTx(ctx,&sql.TxOptions{Isolation:sql.LevelReadCommitted})
	if err!=nil{return Letter{},ErrUnavailable}
	defer tx.Rollback()
	var generation string
	if err=tx.QueryRowContext(ctx,"SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&generation);err!=nil{return Letter{},ErrUnavailable}
	if generation!=l.Generation{return Letter{},ErrChanged}
	err=func()error{
		var exists int64
		if e:=tx.QueryRowContext(ctx,"SELECT id FROM company_vacancies WHERE id=? FOR UPDATE",l.VacancyID).Scan(&exists);e!=nil{
			if errors.Is(e,sql.ErrNoRows){return ErrMissing};return ErrUnavailable
		}
		var expiry int64
		var token,hash string
		if e:=tx.QueryRowContext(ctx,`SELECT owner_token,input_hash,expires_at FROM cover_letter_generation_leases WHERE
		  user_name=? AND vacancy_id=? AND generation=? FOR UPDATE`,l.User,l.VacancyID,l.Generation).
		  Scan(&token,&hash,&expiry);e!=nil||token!=l.Token||hash!=l.InputHash||expiry<=time.Now().UnixMilli(){return ErrChanged}
		current,e:=readInput(ctx,tx,l.User,l.VacancyID)
		if e!=nil{return e}
		if current.Hash()!=in.Hash(){return ErrChanged}
		if previous,has:=current.Effective();has&&!regenerate&&strings.TrimSpace(previous.CoverLetterMD)!=""{return ErrChanged}
		revision:=int64(0)
		if current.HasPersonal {revision=current.PersonalRevision+1}
		override:=0
		if regenerate{override=1}
		// MariaDB baseline не имеет PK для vacancy_cover_letters.
		// Сохраняем ровно одну личную строку под блокировкой вакансии.
		if _,e=tx.ExecContext(ctx,"DELETE FROM vacancy_cover_letters WHERE user_name=? AND vacancy_id=?",l.User,l.VacancyID);e!=nil{return ErrUnavailable}
		_,e=tx.ExecContext(ctx,`INSERT INTO vacancy_cover_letters(user_name,vacancy_id,subject,markdown,override_shared,revision)
		 VALUES(?,?,?,?,?,?)`,l.User,l.VacancyID,answer.Subject,answer.Markdown,override,revision)
		if e!=nil{return ErrUnavailable}
		return nil
	}()
	if err!=nil{return Letter{},err}
	if err=tx.Commit();err!=nil{return Letter{},ErrUnavailable}
	return Letter{answer.Subject,answer.Markdown,"personal"},nil
}
