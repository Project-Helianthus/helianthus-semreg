package driver_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	v1 "github.com/Project-Helianthus/helianthus-semreg/semreg/v1"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// carryOperatorLinks compiles against the publication port alone.
func carryOperatorLinks(tx driver.PublicationTransaction, upserts []v1.NativeBinding) []v1.IdentityLink {
	return tx.OperatorLinks(upserts)
}

func binding(id string) v1.NativeBinding {
	return v1.NativeBinding{
		BindingID: v1.NativeBindingID("binding:" + id), AssetID: "asset:one", SourceID: "source:one", SourceEpochID: "epoch:one",
		DriverGeneration: "1", NativeResource: evidence(id), State: v1.BindingCurrent, Revision: "1",
	}
}

func TestSessionAccessorsAlongsideThePublisher(t *testing.T) {
	session := newSession()
	var s driver.Session = session
	if s.Native() == nil || s.SemanticInput() == nil || s.ScopedOperations() == nil || s.Calendar() == nil || s.Publisher() == nil {
		t.Fatal("a session accessor returned nil")
	}
	if _, ok := s.Native().(*syntheticNative); !ok {
		t.Error("Native does not return the registry")
	}
	if _, ok := s.SemanticInput().(*syntheticInput); !ok {
		t.Error("SemanticInput does not return the scoped input")
	}
	if _, ok := s.ScopedOperations().(*syntheticOperations); !ok {
		t.Error("ScopedOperations does not return the scoped operations")
	}
	if _, ok := s.Calendar().(*syntheticCalendar); !ok {
		t.Error("Calendar does not return the calendar service")
	}
	typ := reflect.TypeOf((*driver.Session)(nil)).Elem()
	var names []string
	for i := 0; i < typ.NumMethod(); i++ {
		names = append(names, typ.Method(i).Name)
	}
	want := []string{"Calendar", "Native", "Prepare", "Publisher", "RegisterExecutor", "ScopedOperations", "SemanticInput"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("session methods = %v", names)
	}
}

func TestOperatorLinksOnThePublicationPort(t *testing.T) {
	session := newSession()
	tx, err := session.Publisher().Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	upserts := []v1.NativeBinding{binding("a"), binding("b")}
	links := carryOperatorLinks(tx, upserts)
	if len(links) != 2 || links[0].BindingID != "binding:a" || links[1].BindingID != "binding:b" {
		t.Fatalf("links = %+v", links)
	}
	for _, l := range links {
		wantValid(t, string(l.BindingID), l.Validate())
	}
	if got := carryOperatorLinks(tx, nil); len(got) != 0 {
		t.Errorf("no upserts returned %d links", len(got))
	}
}

func TestManufacturerSerialAdmissionThroughTheSessionPublisher(t *testing.T) {
	session := newSession()
	tx, err := session.Publisher().Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	claim := driver.ManufacturerSerialClaim{Member: node("a"), Manufacturer: "Maker", Serial: "SN-0001", Proof: evidenceSet("reported")}
	if err := tx.AdmitManufacturerSerial(claim); err != nil {
		t.Fatal(err)
	}
	if got := session.publisher.tx.claims; len(got) != 1 || !reflect.DeepEqual(got[0], claim) {
		t.Errorf("admitted = %+v", got)
	}
	if err := tx.AdmitManufacturerSerial(driver.ManufacturerSerialClaim{Manufacturer: "Maker", Serial: "SN-0001"}); err == nil {
		t.Error("a claim without a member or proof was admitted")
	}
}

// publishThroughThePort compiles a driver publication against the session
// contract alone: a batch, a source, its setting route and the commit.
func publishThroughThePort(ctx context.Context, s driver.Session, b v1.PublicationBatch, src driver.Source, r driver.SettingRoute) error {
	tx, err := s.Publisher().Begin(ctx)
	if err != nil {
		return err
	}
	for _, step := range []error{tx.Stage(b), tx.DeclareSource(src), tx.DeclareSettingRoute(r)} {
		if step != nil {
			_ = tx.Abort(ctx)
			return step
		}
	}
	return tx.Commit(ctx)
}

func TestDriverPublishesAndCommitsThroughTheSessionPort(t *testing.T) {
	session := newSession()
	batch := v1.PublicationBatch{BatchID: "batch:one", AssetID: "asset:one", SourceID: "source:one", SourceEpochID: "epoch:one", DriverGeneration: "1"}
	if err := publishThroughThePort(context.Background(), session, batch, source("flow", 1), settingRoute("flow")); err != nil {
		t.Fatal(err)
	}
	tx := session.publisher.tx
	if !tx.committed || tx.aborted {
		t.Errorf("committed=%v aborted=%v", tx.committed, tx.aborted)
	}
	if len(tx.batches) != 1 || tx.batches[0].BatchID != "batch:one" || len(tx.sources) != 1 || len(tx.routes) != 1 {
		t.Errorf("staged batches=%d sources=%d routes=%d", len(tx.batches), len(tx.sources), len(tx.routes))
	}
}

func TestInvalidStagingAbortsWithoutCommit(t *testing.T) {
	session := newSession()
	err := publishThroughThePort(context.Background(), session, v1.PublicationBatch{}, driver.Source{}, settingRoute("flow"))
	if err == nil {
		t.Fatal("an invalid source was staged")
	}
	tx := session.publisher.tx
	if tx.committed || !tx.aborted {
		t.Errorf("committed=%v aborted=%v", tx.committed, tx.aborted)
	}
}

func TestCalendarPortReturnsTheDonorOperands(t *testing.T) {
	session := newSession()
	calendar := driver.Calendar(session.Calendar())
	acquisition := calendar.BeginAcquisition()
	ref := acquisition.Reference()
	if ref.Instant() != time.Date(2026, time.March, 15, 10, 0, 0, 0, time.UTC) {
		t.Errorf("instant = %v", ref.Instant())
	}
	if got := ref.Date(); got != (driver.CalendarDate{Year: 2026, Month: time.March, Day: 15}) {
		t.Errorf("date = %+v", got)
	}
	if ref.Year() != 2026 || ref.PreviousYear() != 2025 {
		t.Errorf("year = %d, previous %d", ref.Year(), ref.PreviousYear())
	}
	if got := ref.Month(); got != (driver.CalendarMonth{Year: 2026, Month: time.March}) {
		t.Errorf("month = %+v", got)
	}
	if got := ref.PreviousMonth(); got != (driver.CalendarMonth{Year: 2026, Month: time.February}) {
		t.Errorf("previous month = %+v", got)
	}
	if err := acquisition.Check(calendar); err != nil {
		t.Errorf("same-calendar guard: %v", err)
	}
	if err := acquisition.Check(&syntheticCalendar{}); err == nil {
		t.Error("another calendar passed the guard")
	}
	january := syntheticCalendarReference{at: time.Date(2026, time.January, 3, 0, 0, 0, 0, time.UTC)}
	if got := january.PreviousMonth(); got != (driver.CalendarMonth{Year: 2025, Month: time.December}) {
		t.Errorf("previous month across a year = %+v", got)
	}
}
