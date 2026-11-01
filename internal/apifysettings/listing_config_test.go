package apifysettings

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"testing"
)

func TestListingConfigLegacyParity(t *testing.T) {
	cases := []string{`{}`, `null`, `[]`, `{"html":true,"titleClass":"job","allowedHosts":["hh.ru"],"pagination":{"queryParam":"page"}}`, `{"titleClass":"job"}`, `{"adapter":"yoomoney"}`, `{"adapter":"unknown"}`, `{"allowedHosts":["127.0.0.1"]}`, `{"allowedHosts":["metadata.google.internal"]}`, `{"allowedHosts":["HH.ru"]}`, `{"allowedHosts":["hh.ru:443"]}`, `{"pagination":{"queryParam":"page","extra":1}}`, `{"html":null,"adapter":0,"titleClass":false}`, `{"detailPathPrefix":"/jobs/"}`, `{"detailPathPrefix":"/"}`, `{"useEnhancedProxy":"false"}`, `{"unknown":true}`}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node required for legacy parity")
	}
	script := `import {validateListingConfig} from './server/repositories/vacancy-source-parsing.js';let s='';for await(const c of process.stdin)s+=c;const out=JSON.parse(s).map(v=>{try{return {ok:true,value:validateListingConfig(JSON.parse(v))}}catch{return {ok:false}}});console.log(JSON.stringify(out));`
	input, _ := json.Marshal(cases)
	cmd := exec.Command(node, "--input-type=module", "-e", script)
	cmd.Dir = "../.."
	cmd.Stdin = bytes.NewReader(input)
	result, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var expected []struct {
		OK    bool            `json:"ok"`
		Value json.RawMessage `json:"value"`
	}
	if err = json.Unmarshal(result, &expected); err != nil {
		t.Fatal(err)
	}
	for i, raw := range cases {
		actual, e := normalizeListingConfig([]byte(raw))
		if (e == nil) != expected[i].OK {
			t.Fatalf("%s: error %v expected ok=%v", raw, e, expected[i].OK)
		}
		if e == nil {
			var a, b any
			json.Unmarshal(actual, &a)
			json.Unmarshal(expected[i].Value, &b)
			aa, _ := json.Marshal(a)
			bb, _ := json.Marshal(b)
			if string(aa) != string(bb) {
				t.Fatalf("%s: %s != %s", raw, aa, bb)
			}
		}
	}
}
