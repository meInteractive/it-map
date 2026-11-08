package coverletters

import "testing"

func TestAdvancedLetterRequiresNewSavedContent(t *testing.T) {
	base:=Input{
		Generation:"ci-generation",VacancyID:123,
		SharedSubject:"Shared",SharedMD:"Shared content",
		PersonalSubject:"Old",PersonalMD:"Old personal letter",
		PersonalOverride:true,PersonalRevision:4,HasPersonal:true,
		Resume:"CI resume",Prompt:"CI prompt",PromptRevision:2,
	}
	for _,tc:=range []struct{
		name string
		change func(*Input)
		wantOK bool
		wantSubject string
	}{
		{"unchanged",func(*Input){},false,""},
		{"resume updated",func(in *Input){in.Resume="Updated resume";in.ResumeRevision++},false,""},
		{"prompt updated",func(in *Input){in.Prompt="Updated prompt";in.PromptRevision++},false,""},
		{"model updated",func(in *Input){in.Config.Revision++},false,""},
		{"vacancy updated",func(in *Input){in.Title="Updated vacancy"},false,""},
		{"new personal revision",func(in *Input){in.PersonalRevision++;in.PersonalSubject="Winner";in.PersonalMD="New letter"},true,"Winner"},
		{"manual personal overwrite",func(in *Input){in.PersonalMD="New letter";in.PersonalSubject="Manual winner"},true,"Manual winner"},
		{"different shared but personal still effective",func(in *Input){in.SharedMD="New shared content"},false,""},
	}{
		t.Run(tc.name,func(t *testing.T){
			current:=base
			tc.change(&current)
			got,ok:=advancedLetter(base,current)
			if ok!=tc.wantOK || (tc.wantOK && got.CoverLetterSubject!=tc.wantSubject){
				t.Fatalf("got %+v, ok=%t, want ok=%t subject=%s",got,ok,tc.wantOK,tc.wantSubject)
			}
		})
	}
	noPersonal:=base
	noPersonal.HasPersonal=false
	noPersonal.SharedMD=""
	noPersonal.SharedSubject=""
	newPersonal:=noPersonal
	newPersonal.HasPersonal=true
	newPersonal.PersonalOverride=false
	if got,ok:=advancedLetter(noPersonal,newPersonal);!ok||got.CoverLetterMD!="Old personal letter"{
		t.Fatalf("first saved personal letter not recognized: %+v %t",got,ok)
	}
	newShared:=noPersonal
	newShared.SharedSubject="New shared letter"
	newShared.SharedMD="New shared content"
	if got,ok:=advancedLetter(noPersonal,newShared);!ok||got.Source!="vacancy"{
		t.Fatalf("new shared letter not recognized: %+v %t",got,ok)
	}
}
