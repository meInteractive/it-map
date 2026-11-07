package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// FunnelRepository читает факты кандидатов, а не число попыток/строк Dataset.
// События и когорты намеренно имеют разные временные знаменатели.
type FunnelRepository struct {
	DB *sql.DB
	Now func() time.Time
}

var ErrFunnelStorage = errors.New("parser funnel storage unavailable")
var ErrFunnelInput = errors.New("invalid parser funnel filters")

type funnelOption struct {
	ID string `json:"id"`
	Name string `json:"name"`
}
var funnelParsers = []funnelOption{
	{"apify","Apify"}, {"firecrawl","Firecrawl"}, {"http","Прямой HTTP"}, {"unknown","Не определён"},
}
var funnelKeys = [...]string{"received","accepted","fulltext","structured","roles","published"}

type funnelCounts struct {
	Received int64 `json:"received"`
	Accepted int64 `json:"accepted"`
	Fulltext int64 `json:"fulltext"`
	Structured int64 `json:"structured"`
	Roles int64 `json:"roles"`
	Published int64 `json:"published"`
}
func (c *funnelCounts) add(index int) {
	switch index {
	case 0: c.Received++
	case 1: c.Accepted++
	case 2: c.Fulltext++
	case 3: c.Structured++
	case 4: c.Roles++
	case 5: c.Published++
	}
}
func (c funnelCounts) average(days float64) map[string]float64 {
	return map[string]float64{
		"received":float64(c.Received)/days, "accepted":float64(c.Accepted)/days,
		"fulltext":float64(c.Fulltext)/days, "structured":float64(c.Structured)/days,
		"roles":float64(c.Roles)/days, "published":float64(c.Published)/days,
	}
}
type funnelDay struct {
	Day string `json:"day"`
	funnelCounts
}
func (d funnelDay) MarshalJSON() ([]byte,error) {
	return json.Marshal(struct{
		Day string `json:"day"`
		funnelCounts
	}{d.Day,d.funnelCounts})
}
type funnelCohort struct {
	funnelCounts
	Rejected int64 `json:"rejected"`
	Error int64 `json:"error"`
	Waiting int64 `json:"waiting"`
	Created int64 `json:"created"`
	Linked int64 `json:"linked"`
	UndatedPublished int64 `json:"undatedPublished"`
}
func (c funnelCohort) MarshalJSON() ([]byte,error) {
	return json.Marshal(struct{
		funnelCounts
		Rejected int64 `json:"rejected"`
		Error int64 `json:"error"`
		Waiting int64 `json:"waiting"`
		Created int64 `json:"created"`
		Linked int64 `json:"linked"`
		UndatedPublished int64 `json:"undatedPublished"`
	}{c.funnelCounts,c.Rejected,c.Error,c.Waiting,c.Created,c.Linked,c.UndatedPublished})
}

func FunnelFilterValid(source, parser string) bool {
	validSource := source=="all" || source=="unknown"
	for _, option := range SourceOptions() {
		validSource = validSource || source==option.ID
	}
	validParser := parser=="all"
	for _, option := range funnelParsers {
		validParser = validParser || parser==option.ID
	}
	return validSource && validParser
}

var moscow = time.FixedZone("Europe/Moscow",3*3600)
func funnelDate(raw sql.NullString) string {
	if !raw.Valid || raw.String=="" { return "" }
	var at time.Time
	var err error
	for _, layout := range []string{time.RFC3339Nano,"2006-01-02 15:04:05.999999999","2006-01-02 15:04:05","2006-01-02"} {
		at,err = time.Parse(layout,raw.String)
		if err==nil { break }
	}
	if err!=nil { return "" }
	return at.In(moscow).Format("2006-01-02")
}

type funnelCandidate struct {
	Received,Accepted,Fulltext,Structured,Roles,Published sql.NullString
	MainID sql.NullInt64
	Created bool
	ActiveRoles,ResolvedRoles,MatchedRoles int64
	AttemptError bool
}

// Дату первого успеха фиксируем по candidate_id, не по attempts/scan runs.
// Для актуального результата классификации проверяем hash заголовка, модель и
// SHA-256 действующего промпта, чтобы смена промпта не считалась отказом.
const funnelCandidateSQL = `SELECT c.id,
  CASE WHEN i.candidate_token IS NOT NULL THEN COALESCE(i.source_id,'unknown') ELSE COALESCE(c.source_id,'unknown') END intake_source,
  COALESCE(i.parser,'unknown') intake_parser,
  COALESCE(i.received_at,c.first_seen_at) received_at,
  c.main_vacancy_id,
  (SELECT MIN(d.classified_at) FROM vacancy_classifications d WHERE d.candidate_id=c.id AND d.decision='match') accepted_at,
  (SELECT MIN(s.fetched_at) FROM vacancy_raw_snapshots s WHERE s.candidate_id=c.id AND s.content_status='valid') fulltext_at,
  (SELECT MIN(e.processed_at) FROM vacancy_extractions e WHERE e.candidate_id=c.id AND e.status='success') structured_at,
  (SELECT MIN(r.processed_at) FROM vacancy_role_results r WHERE r.candidate_id=c.id AND r.status='success' AND
    CASE WHEN JSON_VALID(r.role_ids) THEN JSON_LENGTH(r.role_ids) ELSE 0 END > 0) roles_at,
  (SELECT MIN(o.processed_at) FROM vacancy_import_operations o WHERE o.candidate_id=c.id AND o.status='success' AND o.action IN ('create','update','unchanged')) published_at,
  EXISTS (SELECT 1 FROM vacancy_import_operations o WHERE o.candidate_id=c.id AND o.status='success' AND o.action='create') created,
  (SELECT COUNT(*) FROM vacancy_role_classification rc JOIN vacancy_roles vr ON vr.id=rc.role_id
   WHERE vr.parent_id IS NULL AND rc.parsing_enabled=1 AND CHAR_LENGTH(TRIM(rc.markdown))>0) active_roles,
  (SELECT COUNT(*) FROM vacancy_role_classification rc JOIN vacancy_roles vr ON vr.id=rc.role_id
   WHERE vr.parent_id IS NULL AND rc.parsing_enabled=1 AND CHAR_LENGTH(TRIM(rc.markdown))>0
   AND EXISTS(SELECT 1 FROM vacancy_classifications d WHERE d.candidate_id=c.id
    AND d.root_role_id=rc.role_id AND d.title_hash=c.title_hash AND d.title_revision=c.title_revision
    AND BINARY d.model_key=BINARY 'typesafe/jev-1.13' AND BINARY d.prompt_version=BINARY LOWER(SHA2(rc.markdown,256)))) resolved_roles,
  (SELECT COUNT(*) FROM vacancy_role_classification rc JOIN vacancy_roles vr ON vr.id=rc.role_id
   WHERE vr.parent_id IS NULL AND rc.parsing_enabled=1 AND CHAR_LENGTH(TRIM(rc.markdown))>0
   AND (SELECT d.decision FROM vacancy_classifications d WHERE d.candidate_id=c.id
    AND d.root_role_id=rc.role_id AND d.title_hash=c.title_hash AND d.title_revision=c.title_revision
    AND BINARY d.model_key=BINARY 'typesafe/jev-1.13' AND BINARY d.prompt_version=BINARY LOWER(SHA2(rc.markdown,256))
    ORDER BY d.id DESC LIMIT 1)='match') matched_roles,
  EXISTS(SELECT 1 FROM vacancy_classification_attempts t
    WHERE t.candidate_id=c.id AND t.title_hash=c.title_hash AND t.title_revision=c.title_revision
    AND BINARY t.model_key=BINARY 'typesafe/jev-1.13' AND JSON_LENGTH(t.errors_json)>0) attempt_error
 FROM vacancy_candidates c LEFT JOIN parser_candidate_intakes i ON i.candidate_token=c.creation_token
 WHERE (i.parser IN ('apify','firecrawl','http','unknown') OR
   (i.parser IS NULL AND c.acquisition_method IN ('site','firecrawl')))
 AND (?='all' OR CASE WHEN i.candidate_token IS NOT NULL THEN COALESCE(i.source_id,'unknown') ELSE COALESCE(c.source_id,'unknown') END=?)
 AND (?='all' OR COALESCE(i.parser,'unknown')=?)
 ORDER BY c.id`

// Ошибки очереди привязаны к текущему поколению и активному состоянию.
// Старые сбои с более поздним успешным/работающим повтором не считаются.
const funnelJobSQL = `SELECT j.candidate_id,j.status,j.reason_code FROM scheduler_jobs j
 WHERE j.generation=? AND j.candidate_id IS NOT NULL AND j.status IN ('failed','pending')
 AND NOT EXISTS(SELECT 1 FROM scheduler_jobs n
  WHERE n.generation=j.generation AND n.candidate_id=j.candidate_id AND n.type=j.type
    AND n.status IN ('completed','running','pending')
    AND (n.updated_at>j.updated_at OR (n.updated_at=j.updated_at AND BINARY n.id>BINARY j.id)))`

func (r FunnelRepository) ReadFunnel(ctx context.Context, sourceID, parserID string) (json.RawMessage,error) {
	if !FunnelFilterValid(sourceID,parserID) { return nil,ErrFunnelInput }
	if r.DB==nil { return nil,ErrFunnelStorage }
	now:=time.Now()
	if r.Now!=nil { now=r.Now() }
	days:=make([]funnelDay,30)
	index:=make(map[string]int,30)
	local:=now.In(moscow)
	for i:=range days {
		key:=local.AddDate(0,0,i-29).Format("2006-01-02")
		days[i].Day=key
		index[key]=i
	}
	from,to:=days[0].Day,days[len(days)-1].Day
	tx,err:=r.DB.BeginTx(ctx,&sql.TxOptions{ReadOnly:true,Isolation:sql.LevelRepeatableRead})
	if err!=nil { return nil,ErrFunnelStorage }
	defer tx.Rollback()
	var generation string
	if err=tx.QueryRowContext(ctx,"SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); err!=nil || !generationPattern.MatchString(generation) { return nil,ErrFunnelStorage }
	// Факты из разных поколений сохраняются; только активные ошибки очереди
	// берём из текущего поколения.
	rows,err:=tx.QueryContext(ctx,funnelCandidateSQL,sourceID,sourceID,parserID,parserID)
	if err!=nil { return nil,ErrFunnelStorage }
	type candidateEntry struct{ id int64; fact funnelCandidate }
	candidates:=[]candidateEntry{}
	for rows.Next() {
		var entry candidateEntry
		var source,parser string
		var created,attemptErr int64
		f:=&entry.fact
		err=rows.Scan(&entry.id,&source,&parser,&f.Received,&f.MainID,
			&f.Accepted,&f.Fulltext,&f.Structured,&f.Roles,&f.Published,
			&created,&f.ActiveRoles,&f.ResolvedRoles,&f.MatchedRoles,&attemptErr)
		if err!=nil { break }
		f.Created=created!=0
		f.AttemptError=attemptErr!=0
		candidates=append(candidates,entry)
	}
	if err==nil { err=rows.Err() }
	rows.Close()
	if err!=nil { return nil,ErrFunnelStorage }
	jobErrors:=map[int64]bool{}
	jobs,err:=tx.QueryContext(ctx,funnelJobSQL,generation)
	if err!=nil { return nil,ErrFunnelStorage }
	for jobs.Next() {
		var id int64
		var status,reason string
		if err=jobs.Scan(&id,&status,&reason); err!=nil { break }
		if status=="failed" || !expectedIssueWait[reason] { jobErrors[id]=true }
	}
	if err==nil { err=jobs.Err() }
	jobs.Close()
	if err!=nil { return nil,ErrFunnelStorage }
	if err=tx.Commit();err!=nil {return nil,ErrFunnelStorage}
	totals,cohort:=funnelCounts{},funnelCohort{}
	for _, entry:=range candidates {
		f:=entry.fact
		date:=[...]string{funnelDate(f.Received),funnelDate(f.Accepted),funnelDate(f.Fulltext),
			funnelDate(f.Structured),funnelDate(f.Roles),funnelDate(f.Published)}
		for stage,d:=range date {
			if i,ok:=index[d];ok {
				days[i].funnelCounts.add(stage)
				totals.add(stage)
			}
		}
		if date[0]<from || date[0]>to {continue}
		cohort.funnelCounts.add(0)
		for stage:=1;stage<len(date);stage++{
			if date[stage]!="" || (stage==5 && f.MainID.Valid) {cohort.funnelCounts.add(stage)}
		}
		if f.Published.Valid || f.MainID.Valid {
			if f.Created {cohort.Created++
			} else if f.Published.Valid {cohort.Linked++}
			if !f.Published.Valid {cohort.UndatedPublished++}
		} else if f.ActiveRoles>0 && f.ResolvedRoles==f.ActiveRoles && f.MatchedRoles==0 {
			cohort.Rejected++
		} else if (f.ActiveRoles>f.ResolvedRoles && f.AttemptError) || jobErrors[entry.id] {
			cohort.Error++
		} else {cohort.Waiting++}
	}
	sources:=make([]funnelOption,0,len(SourceOptions())+1)
	for _, s:=range SourceOptions(){ sources=append(sources,funnelOption{s.ID,s.Name}) }
	sources=append(sources,funnelOption{"unknown","Источник не указан"})
	body:=struct{
		CheckedAt int64 `json:"checkedAt"`
		Timezone string `json:"timezone"`
		From string `json:"from"`
		To string `json:"to"`
		Days int `json:"days"`
		Filters map[string]string `json:"filters"`
		SourceOptions []funnelOption `json:"sourceOptions"`
		ParserOptions []funnelOption `json:"parserOptions"`
		Daily []funnelDay `json:"daily"`
		Totals funnelCounts `json:"totals"`
		Averages map[string]float64 `json:"averages"`
		Cohort funnelCohort `json:"cohort"`
	}{now.UnixMilli(),"Europe/Moscow",from,to,30,map[string]string{"sourceId":sourceID,"parser":parserID},sources,funnelParsers,days,totals,totals.average(30),cohort}
	encoded,e:=json.Marshal(body)
	if e!=nil {return nil,ErrFunnelStorage}
	return encoded,nil
}
