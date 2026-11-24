package life

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func withheldAssertRefusals(t *testing.T, svc *Service, id InstanceID, code RefusalCode, retryable bool, reason string) {
	t.Helper()
	for _, method := range []Method{MethodStart, MethodStop, MethodRestart} {
		t.Run(string(method), func(t *testing.T) {
			_, err := svc.Control(deadlineCtx(t), method, body(id, 1))
			var refusal *Refusal
			if !errors.As(err, &refusal) {
				t.Fatalf("refusal = %v", err)
			}
			if refusal.Code != code || refusal.Retryable() != retryable || refusal.Reason != reason || refusal.Row != 3 {
				t.Fatalf("refusal = %+v, retryable=%v; want %s, retryable=%v, reason=%q, row=3", refusal, refusal.Retryable(), code, retryable, reason)
			}
		})
	}
}

func TestDurableWithholdingPrecondition(t *testing.T) {
	t.Parallel()
	for _, cause := range []struct {
		name        string
		content     string
		disposition ControlLoadDisposition
	}{
		{"corrupt", "{synthetic-corruption", ControlLoadCorrupt},
		{"unsupported", `{"schema_version":2,"issued_revision_mark":"0","catalog_revision":"1","instances":{}}`, ControlLoadUnsupported},
	} {
		for _, configured := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/enabled=%v", cause.name, configured), func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, controlFileName)
				if err := os.WriteFile(path, []byte(cause.content), 0o600); err != nil {
					t.Fatal(err)
				}
				id := InstanceID("fam.alpha")
				store, err := OpenFileStore(dir)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = store.Stop(context.Background()) })
				if got, _ := store.ControlLoad(); got != cause.disposition {
					t.Fatalf("disposition = %s, want %s", got, cause.disposition)
				}
				if _, err := store.Register(context.Background(), []ControlInstance{{InstanceID: id, Enabled: configured}}); err != nil {
					t.Fatalf("register = %v", err)
				}
				if raw, _ := os.ReadFile(path); string(raw) != cause.content {
					t.Fatal("a failed control state was rewritten")
				}
				exec := &fakeExec{}
				svc, err := NewService(ServiceOptions{Store: &managerStore{m: store}, Recovery: &fakeRecovery{}, Incarnation: "withheld", Drivers: []ServiceInstance{{InstanceID: id, Executor: exec}}})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(svc.Close)
				if err := svc.Start(context.Background()); err == nil {
					t.Fatal("durable withholding started service")
				}
				withheldAssertRefusals(t, svc, id, RefusalFailedPrecondition, false, "control state is not loaded: "+string(cause.disposition))
				view, err := svc.Get(id)
				if err != nil || view.Enabled != configured || view.DesiredState != DesiredStopped || view.ObservedState != ObservedFailed {
					t.Fatalf("withheld view = %+v, %v; want enabled=%v, STOPPED, FAILED", view, err, configured)
				}
				if calls := exec.snapshot(); len(calls) != 0 {
					t.Fatalf("withheld executor calls = %+v", calls)
				}
			})
		}
	}
}

type withheldCatalogErrorStore struct {
	Store
	err error
}

func (s withheldCatalogErrorStore) Catalog() (ControlCatalog, error) { return ControlCatalog{}, s.err }

func TestTransientStorePreconditionClassification(t *testing.T) {
	t.Parallel()
	transient := errors.New("synthetic transient store failure")
	f := newFixture(t, svcOpts{drivers: enabled("fam.alpha"), noStart: true, wrap: func(s Store) Store { return withheldCatalogErrorStore{Store: s, err: transient} }})
	if err := f.svc.Start(context.Background()); !errors.Is(err, transient) {
		t.Fatalf("Start = %v", err)
	}
	withheldAssertRefusals(t, f.svc, "fam.alpha", RefusalUnavailable, true, "control service not running")
	if calls := f.execs["fam.alpha"].snapshot(); len(calls) != 0 {
		t.Fatalf("executor calls = %+v", calls)
	}
}

func TestWithheldViewPreconditionPreservesIntent(t *testing.T) {
	t.Parallel()
	for _, readable := range []bool{false, true} {
		for _, configured := range []bool{false, true} {
			for _, desired := range []DesiredState{DesiredRunning, DesiredStopped} {
				t.Run(fmt.Sprintf("readable=%v/enabled=%v/desired=%s", readable, configured, desired), func(t *testing.T) {
					f := newFixture(t, svcOpts{
						drivers: []ControlInstance{{InstanceID: "fam.alpha", Enabled: configured}},
						state:   controlState(7, 11, map[string]string{"fam.alpha": recordJSON(desired, 5, 0, "")}, ""),
						noStart: true,
						wrap: func(s Store) Store {
							if readable {
								return roStore{Store: s}
							}
							return withheldCatalogErrorStore{Store: s, err: &Refusal{Code: RefusalFailedPrecondition, Row: 3, Reason: "synthetic durable withholding"}}
						},
					})
					if err := f.svc.Start(context.Background()); err == nil {
						t.Fatal("withheld service started")
					}
					reason := "synthetic durable withholding"
					if readable {
						reason = "control state is read-only"
					}
					withheldAssertRefusals(t, f.svc, "fam.alpha", RefusalFailedPrecondition, false, reason)
					view, err := f.svc.Get("fam.alpha")
					if err != nil || view.Enabled != configured || view.DesiredState != desired || view.DesiredRevision != 5 || view.ObservedState != ObservedFailed {
						t.Fatalf("view = %+v, %v; want enabled=%v, desired=%s, revision=5, FAILED", view, err, configured, desired)
					}
					if calls := f.execs["fam.alpha"].snapshot(); len(calls) != 0 {
						t.Fatalf("executor calls = %+v", calls)
					}
				})
			}
		}
	}
}
