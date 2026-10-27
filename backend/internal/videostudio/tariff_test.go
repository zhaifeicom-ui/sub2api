package videostudio

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConditionalTariffsAndRounding(t *testing.T) {
	c := testChannel()
	m := &c.Models[0]
	m.Rules = []Tariff{{Resolution: "768p", MinSeconds: 4, MaxSeconds: 30, Audio: "off", Reference: "any", Mode: "second", Rate: 2, CostCNY: .2, MinimumSeconds: 6, StepSeconds: 4, BaseFee: 1, BaseCostCNY: .1, ImageFee: .5, ImageCostCNY: .05}}
	r := testRequest()
	r.Input.Duration = 5
	r.Input.Images = []string{"asset-1", "asset-2"}
	p, e := Quote(c, r)
	if e != nil {
		t.Fatal(e)
	}
	if p.Amount != 18 || p.UpstreamCostCNY != 1.8 {
		t.Fatalf("wrong rounded tariff: %+v", p)
	}
	r.Input.GenerateAudio = true
	if _, e = Quote(c, r); e == nil {
		t.Fatal("unpriced sound combination accepted")
	}
	m.Rules = append(m.Rules, m.Rules[0])
	if e = c.Validate(); e == nil {
		t.Fatal("overlapping tariff saved")
	}
	r.Input.GenerateAudio = false
	if _, e = Quote(c, r); e == nil {
		t.Fatal("overlapping tariff billed")
	}
}
func TestPerJobTariffAndPublicCostRedaction(t *testing.T) {
	c := testChannel()
	m := &c.Models[0]
	m.Rules = []Tariff{{Resolution: "768p", MinSeconds: 4, MaxSeconds: 30, Audio: "any", Reference: "any", Mode: "job", Rate: 3, CostCNY: 2, BaseFee: 1, BaseCostCNY: .1, AudioFee: .5, AudioCostCNY: .2}}
	r := testRequest()
	r.Input.Duration = 10
	r.Input.Audios = []string{"a"}
	p, e := Quote(c, r)
	if e != nil {
		t.Fatal(e)
	}
	if p.Amount != 4.5 {
		t.Fatalf("job was multiplied by duration: %v", p.Amount)
	}
	b, _ := json.Marshal(PublicModel(*m))
	if strings.Contains(string(b), "cost_cny") {
		t.Fatalf("cost leaked: %s", b)
	}
	if m.Rules[0].CostCNY != 2 {
		t.Fatal("redaction modified stored cost")
	}
}

func TestSubQuantumPriceCannotGenerateForFree(t *testing.T) {
	c := testChannel()
	c.Models[0].Rates["768p"] = 1e-12
	if _, e := Quote(c, testRequest()); e == nil {
		t.Fatal("sub-quantum price accepted as free")
	}
}
