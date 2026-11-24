package driver_test

import (
	"testing"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

func TestMatchClaim(t *testing.T) {
	c := driver.MatchClaim{Member: node("a"), Scheme: "scheme.test.serial", Key: digestOf("token"), Proof: evidenceSet("proof")}
	wantValid(t, "claim", c.Validate())
	c.Issuer = "issuer.test"
	wantValid(t, "claim with issuer", c.Validate())
	for name, mutate := range map[string]func(*driver.MatchClaim){
		"member": func(c *driver.MatchClaim) { c.Member = driver.NodeRef{} },
		"scheme": func(c *driver.MatchClaim) { c.Scheme = "" },
		"key":    func(c *driver.MatchClaim) { c.Key = "name:boiler" },
		"proof":  func(c *driver.MatchClaim) { c.Proof = nil },
	} {
		bad := c
		mutate(&bad)
		wantInvalid(t, "claim without a valid "+name, bad.Validate())
	}
}

func TestManufacturerSerialClaimIsItsOwnRecord(t *testing.T) {
	c := driver.ManufacturerSerialClaim{Member: node("a"), Manufacturer: "Maker", Serial: "SN-0001", Proof: evidenceSet("reported")}
	wantValid(t, "complete claim", c.Validate())
	missing := c
	missing.Serial = ""
	wantValid(t, "claim with a missing component stays representable", missing.Validate())
	wantInvalid(t, "claim without member", driver.ManufacturerSerialClaim{Manufacturer: "Maker", Serial: "SN-0001", Proof: evidenceSet("reported")}.Validate())
	wantInvalid(t, "claim without proof", driver.ManufacturerSerialClaim{Member: node("a"), Manufacturer: "Maker", Serial: "SN-0001"}.Validate())
	wantInvalid(t, "claim with invalid proof", driver.ManufacturerSerialClaim{Member: node("a"), Proof: []v1.EvidenceRef{{}}}.Validate())
}

func statement() driver.StatementRef {
	return driver.StatementRef{Revision: "3", Evidence: evidence("statement")}
}

func TestCertifiedJoin(t *testing.T) {
	j := driver.CertifiedJoin{A: node("a"), B: node("b"), Survivor: node("b"), Statement: statement()}
	wantValid(t, "join", j.Validate())
	for name, mutate := range map[string]func(*driver.CertifiedJoin){
		"a":                  func(j *driver.CertifiedJoin) { j.A = driver.NodeRef{} },
		"b":                  func(j *driver.CertifiedJoin) { j.B = driver.NodeRef{} },
		"survivor":           func(j *driver.CertifiedJoin) { j.Survivor = driver.NodeRef{} },
		"statement":          func(j *driver.CertifiedJoin) { j.Statement = driver.StatementRef{} },
		"statement revision": func(j *driver.CertifiedJoin) { j.Statement.Revision = "-1" },
		"statement origin":   func(j *driver.CertifiedJoin) { j.Statement.Evidence = v1.EvidenceRef{} },
	} {
		bad := j
		mutate(&bad)
		wantInvalid(t, "join with invalid "+name, bad.Validate())
	}
	same := j
	same.B = same.A
	same.Survivor = same.A
	wantInvalid(t, "join of a member with itself", same.Validate())
	foreign := j
	foreign.Survivor = node("c")
	wantInvalid(t, "join with a survivor outside the pair", foreign.Validate())
}
