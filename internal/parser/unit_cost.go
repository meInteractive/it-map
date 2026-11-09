package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"time"
)

// GO-433 is a calendar-spend diagnostic, NOT a verified acquisition-cohort
// cost. Missing provider identities prevent reliable source/scan attribution.
var ErrUnitCostInput = errors.New("invalid vacancy unit-cost filters")
var ErrUnitCostStorage = errors.New("vacancy unit-cost storage unavailable")

type UnitCostRepository struct {
	DB *sql.DB
	Now func() time.Time
}

type unitCostSpend struct {
	ConfirmedUSD float64 `json:"confirmedUsd"`
	ConfirmedOperations int64 `json:"confirmedOperations"`
	UnknownReservedUSD float64 `json:"unknownReservedUsd"`
	UnknownOperations int64 `json:"unknownOperations"`
	UnpricedOperations int64 `json:"unpricedOperations"`
	ConflictingOperations int64 `json:"conflictingOperations"`
	UnavailableForSource bool `json:"unavailableForSource"`
}

type unitCostDay struct {
	Day string `json:"day"`
	NewVacancies int64 `json:"newVacancies"`
	AIUSD float64 `json:"aiUsd"`
	ApifyUSD float64 `json:"apifyUsd"`
	ObservedCredits float64 `json:"observedCredits"`
}

type unitCostReport struct {
	CheckedAt int64 `json:"checkedAt"`
	Timezone string `json:"timezone"`
	From string `json:"from"`
	To string `json:"to"`
	PeriodDays int `json:"periodDays"`
	SourceID string `json:"sourceId"`
	SourceOptions []funnelOption `json:"sourceOptions"`
	NewUniqueVacancies int64 `json:"newUniqueVacancies"`
	UndatedCreates int64 `json:"undatedCreates"`
	Apify unitCostSpend `json:"apify"`
	AI unitCostSpend `json:"ai"`
	FirecrawlCredits float64 `json:"firecrawlCredits"`
	UnknownCreditRows int64 `json:"unknownCreditRows"`
	TotalConfirmedUSD float64 `json:"totalConfirmedUsd"`
	PartialPeriodRatioUSD *float64 `json:"partialPeriodRatioUsd"`
	// The numerator is calendar spending, the denominator is calendar first
	// creates. Their origins are NOT yet connected as one acquisition cohort.
	CalendarProxy bool `json:"calendarProxy"`
	IncompleteCosts bool `json:"incompleteCosts"`
	Daily []unitCostDay `json:"daily"`
}

func UnitCostFilterValid(days int, source string) bool {
	if days!=7 && days!=30 { return false }
	if source=="all" || source=="unknown" { return true }
	for _, option := range SourceOptions() {
		if option.ID==source { return true }
	}
	return false
}

type unitCostWindow struct {
	start, end int64
	index map[string]int
}

func unitCostWindowFor(now time.Time, days int) (unitCostWindow, []unitCostDay) {
	local:=now.In(moscow)
	today:=time.Date(local.Year(),local.Month(),local.Day(),0,0,0,0,moscow)
	first:=today.AddDate(0,0,1-days)
	index:=make(map[string]int,days)
	daily:=make([]unitCostDay,days)
	for i:=range daily {
		day:=first.AddDate(0,0,i).Format("2006-01-02")
		daily[i].Day=day
		index[day]=i
	}
	return unitCostWindow{start:first.UnixMilli(),end:today.AddDate(0,0,1).UnixMilli(),index:index},daily
}

type unitCostActor struct {
	first int64
	cost *float64
	reserve float64
	conflict bool
}

func (r UnitCostRepository) ReadUnitCost(ctx context.Context, days int, source string) (json.RawMessage,error) {
	if !UnitCostFilterValid(days,source) {return nil,ErrUnitCostInput}
	if r.DB==nil{return nil,ErrUnitCostStorage}
	now:=time.Now().UTC()
	if r.Now!=nil{now=r.Now()}
	window,daily:=unitCostWindowFor(now,days)
	report:=unitCostReport{
		CheckedAt:now.UnixMilli(),Timezone:"Europe/Moscow",
		From:daily[0].Day,To:daily[len(daily)-1].Day,PeriodDays:days,
		SourceID:source,CalendarProxy:true,IncompleteCosts:true,Daily:daily,
		SourceOptions:[]funnelOption{},
	}
	for _,opt:=range SourceOptions(){report.SourceOptions=append(report.SourceOptions,funnelOption{ID:opt.ID,Name:opt.Name})}
	report.SourceOptions=append(report.SourceOptions,funnelOption{ID:"unknown",Name:"Источник не указан"})
	tx,err:=r.DB.BeginTx(ctx,&sql.TxOptions{ReadOnly:true,Isolation:sql.LevelRepeatableRead})
	if err!=nil{return nil,ErrUnitCostStorage}
	defer tx.Rollback()

	// First successful CREATE of each actual main vacancy, across ALL history.
	// Ordering the entire history prevents a retry in this period from
	// duplicating a vacancy first created in an earlier period.
	creates,err:=tx.QueryContext(ctx,`SELECT o.vacancy_id,o.processed_at,
	 COALESCE(i.source_id,c.source_id,'unknown') AS source_id,
	 COALESCE(i.parser,'unknown') AS parser_id,c.acquisition_method
	 FROM vacancy_import_operations o
	 JOIN company_vacancies v ON v.id=o.vacancy_id
	 JOIN vacancy_candidates c ON c.id=o.candidate_id
	 LEFT JOIN parser_candidate_intakes i ON i.candidate_token=c.creation_token
	 WHERE o.status='success' AND o.action='create'
	 ORDER BY o.vacancy_id,o.processed_at,BINARY o.id`)
	if err!=nil{return nil,ErrUnitCostStorage}
	var lastVacancy int64=-1
	for creates.Next(){
		var vacancy int64
		var stamped,origin,parserID,method string
		if err=creates.Scan(&vacancy,&stamped,&origin,&parserID,&method);err!=nil{break}
		if vacancy==lastVacancy{continue}
		lastVacancy=vacancy
		if parserID=="manual" || parserID=="excel" || (parserID=="unknown" && method!="site" && method!="firecrawl"){continue}
		if source!="all" && source!=origin{continue}
		date:=funnelDate(sql.NullString{String:stamped,Valid:true})
		if date==""{report.UndatedCreates++;continue}
		if i,ok:=window.index[date];ok{
			report.NewUniqueVacancies++
			report.Daily[i].NewVacancies++
		}
	}
	if err==nil{err=creates.Err()}
	creates.Close()
	if err!=nil{return nil,ErrUnitCostStorage}

	// A provider Actor may leave several local receipts. Collapse using
	// (account_id,run_id), NEVER the scheduler cycle run_id or dataset_id.
	// We read previous periods too so late duplicate receipts cannot be
	// counted as a new run within the current period.
	actors,err:=tx.QueryContext(ctx,`SELECT id,account_id,run_id,created_at,reserved_usd,charged_usd
	 FROM apify_runs WHERE created_at<? ORDER BY created_at,BINARY id`,window.end)
	if err!=nil{return nil,ErrUnitCostStorage}
	operations:=map[string]*unitCostActor{}
	for actors.Next(){
		var id,account,runID string
		var at int64
		var reserve float64
		var charge sql.NullFloat64
		if err=actors.Scan(&id,&account,&runID,&at,&reserve,&charge);err!=nil{break}
		key:="local:"+id
		if runID!=""{key="actor:"+strconv.Itoa(len(account))+":"+account+":"+runID}
		op:=operations[key]
		if op==nil{
			op=&unitCostActor{first:at}
			operations[key]=op
		}
		if at<op.first{op.first=at}
		if !finiteMoney(reserve){op.conflict=true}else if reserve>op.reserve{op.reserve=reserve}
		if charge.Valid{
			if !finiteMoney(charge.Float64){op.conflict=true
			}else if op.cost!=nil && *op.cost!=charge.Float64{
				op.conflict=true
			}else{
				value:=charge.Float64
				op.cost=&value
			}
		}
	}
	if err==nil{err=actors.Err()}
	actors.Close()
	if err!=nil{return nil,ErrUnitCostStorage}
	if source!="all"{report.Apify.UnavailableForSource=true}
	for _,op:=range operations {
		if op.first<window.start || op.first>=window.end || source!="all"{continue}
		if op.conflict {report.Apify.ConflictingOperations++;continue}
		if op.cost!=nil {
			report.Apify.ConfirmedOperations++
			report.Apify.ConfirmedUSD+=*op.cost
			date:=time.UnixMilli(op.first).In(moscow).Format("2006-01-02")
			if i,ok:=window.index[date];ok{report.Daily[i].ApifyUSD+=*op.cost}
		}else if op.reserve>0{
			report.Apify.UnknownOperations++
			report.Apify.UnknownReservedUSD+=op.reserve
		}else{report.Apify.UnpricedOperations++}
	}

	// The id is the local scheduler billing receipt, NOT a per-candidate or
	// per-role paid operation. Failed/empty requests remain part of spend.
	ai,err:=tx.QueryContext(ctx,`SELECT COALESCE(source_id,'unknown'),started_at,reserved_usd,cost_usd
	 FROM scheduler_attempts
	 WHERE started_at>=? AND started_at<? AND provider IN ('openai','openrouter')`,window.start,window.end)
	if err!=nil{return nil,ErrUnitCostStorage}
	for ai.Next(){
		var origin string
		var at int64
		var reserve float64
		var cost sql.NullFloat64
		if err=ai.Scan(&origin,&at,&reserve,&cost);err!=nil{break}
		if source!="all"&&source!=origin{continue}
		if !finiteMoney(reserve) || (cost.Valid&&!finiteMoney(cost.Float64)){
			report.AI.ConflictingOperations++;continue
		}
		if cost.Valid{
			report.AI.ConfirmedOperations++
			report.AI.ConfirmedUSD+=cost.Float64
			date:=time.UnixMilli(at).In(moscow).Format("2006-01-02")
			if i,ok:=window.index[date];ok{report.Daily[i].AIUSD+=cost.Float64}
		}else if reserve>0{
			report.AI.UnknownOperations++
			report.AI.UnknownReservedUSD+=reserve
		}else{report.AI.UnpricedOperations++}
	}
	if err==nil{err=ai.Err()}
	ai.Close()
	if err!=nil{return nil,ErrUnitCostStorage}

	// Snapshot credits are NOT a Firecrawl invoice. Keep in credit units and
	// do not assign them a USD price or imply each snapshot was a paid call.
	credits,err:=tx.QueryContext(ctx,`SELECT s.fetched_at,s.firecrawl_credits,
	 COALESCE(i.source_id,c.source_id,'unknown')
	 FROM vacancy_raw_snapshots s
	 JOIN vacancy_candidates c ON c.id=s.candidate_id
	 LEFT JOIN parser_candidate_intakes i ON i.candidate_token=c.creation_token`)
	if err!=nil{return nil,ErrUnitCostStorage}
	for credits.Next(){
		var at,origin string
		var value sql.NullFloat64
		if err=credits.Scan(&at,&value,&origin);err!=nil{break}
		if source!="all"&&source!=origin{continue}
		i,ok:=window.index[funnelDate(sql.NullString{String:at,Valid:true})]
		if !ok{continue}
		if !value.Valid || math.IsNaN(value.Float64) || math.IsInf(value.Float64,0) || value.Float64<0{
			report.UnknownCreditRows++
		}else{
			report.FirecrawlCredits+=value.Float64
			report.Daily[i].ObservedCredits+=value.Float64
		}
	}
	if err==nil{err=credits.Err()}
	credits.Close()
	if err!=nil{return nil,ErrUnitCostStorage}

	report.TotalConfirmedUSD=report.AI.ConfirmedUSD+report.Apify.ConfirmedUSD
	if report.NewUniqueVacancies>0 && report.AI.ConfirmedOperations+report.Apify.ConfirmedOperations>0 {
		value:=report.TotalConfirmedUSD/float64(report.NewUniqueVacancies)
		report.PartialPeriodRatioUSD=&value
	}
	if err=tx.Commit();err!=nil{return nil,ErrUnitCostStorage}
	body,err:=json.Marshal(report)
	if err!=nil{return nil,ErrUnitCostStorage}
	return body,nil
}
