package host

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/life"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/runtime/tree"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/catalog"
	"github.com/Project-Helianthus/helianthus-semreg/semreg/v1/driver"
)

// InstanceConfig is one configured instance: its driver kind, its configured
// key, whether it is enabled, the two byte operands the selected factory
// decodes, and its optional opaque secret reference.
type InstanceConfig struct {
	Kind      driver.DriverKind  `json:"kind"`
	Instance  driver.InstanceKey `json:"instance"`
	Enabled   bool               `json:"enabled"`
	Transport json.RawMessage    `json:"transport,omitempty"`
	Options   json.RawMessage    `json:"options,omitempty"`
	SecretRef string             `json:"secret_ref,omitempty"`
}

// Config is the configuration envelope of a host. Policy and secret references
// stay uninterpreted. OperatorIdentityStatements is the trusted typed handoff of
// the embedding application: it has no wire form and no file can populate it.
type Config struct {
	InstallationKey            string                              `json:"installation_key,omitempty"`
	BridgeID                   driver.BridgeID                     `json:"bridge_id,omitempty"`
	StateDir                   string                              `json:"state_dir,omitempty"`
	Clock                      life.Clock                          `json:"-"`
	Calendar                   *string                             `json:"calendar,omitempty"`
	PolicyRefs                 []string                            `json:"policy_refs,omitempty"`
	SecretRefs                 []string                            `json:"secret_refs,omitempty"`
	Instances                  []InstanceConfig                    `json:"instances,omitempty"`
	OperatorIdentityStatements []runtime.OperatorIdentityStatement `json:"-"`
}

// MethodSelection is the application's typed method selection: the exact tree
// method and its typed input.
type MethodSelection struct {
	Method tree.MethodRef `json:"method"`
	Input  tree.Input     `json:"input"`
}

// Task is the application input. Selection is empty on a walk-only run.
type Task struct {
	Selection *MethodSelection
}

// Application is the program a host runs once its instances are started.
type Application func(ctx context.Context, site *Site, task Task) error

// environment is the host's boundary to the process.
type environment struct {
	root   func() (string, error)
	stderr io.Writer
}

func defaultEnvironment() environment {
	return environment{root: defaultRoot, stderr: os.Stderr}
}

// defaultRoot is the per-user configuration directory with a helianthus
// subdirectory.
func defaultRoot() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "helianthus"), nil
}

// DispatchEarly asks only the selected factories that implement
// driver.EarlyDispatcher, in order, and passes through the first handled result
// or error. It reads no configuration, plans or constructs no instance and
// touches no transport. On no match the caller continues ordinary startup.
func DispatchEarly(ctx context.Context, argv []string, selected ...driver.Factory) (handled bool, exitCode int, err error) {
	for _, factory := range selected {
		dispatcher, ok := factory.(driver.EarlyDispatcher)
		if !ok {
			continue
		}
		handled, exitCode, err = dispatcher.DispatchEarly(ctx, append([]string(nil), argv...))
		if handled || err != nil {
			return handled, exitCode, err
		}
	}
	return false, 0, nil
}

// Main is the host program entry. It dispatches early commands on the original
// argv, then reads `<program> [installation.json [selection.json|-]]`, opens and
// starts the host, waits for the initial publications, runs the application and
// closes the same runtime. It returns the application's exit result.
func Main(argv []string, app Application, selected ...driver.Factory) int {
	return run(defaultEnvironment(), argv, app, selected...)
}

func run(env environment, argv []string, app Application, selected ...driver.Factory) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	handled, code, err := DispatchEarly(ctx, argv, selected...)
	if err != nil {
		fmt.Fprintln(env.stderr, "host:", err)
		if code == 0 {
			code = 1
		}
		return code
	}
	if handled {
		return code
	}
	var args []string
	if len(argv) > 1 {
		args = argv[1:]
	}
	if len(args) > 2 {
		fmt.Fprintln(env.stderr, "host: usage: <program> [installation.json [selection.json|-]]")
		return 2
	}
	cfg, err := loadConfig(env, args)
	if err != nil {
		fmt.Fprintln(env.stderr, "host:", err)
		return 1
	}
	var task Task
	if len(args) == 2 && args[1] != "-" {
		selection, err := loadSelection(args[1])
		if err != nil {
			fmt.Fprintln(env.stderr, "host:", err)
			return 1
		}
		task.Selection = selection
	}
	site, err := open(ctx, env, cfg, selected)
	if err != nil {
		fmt.Fprintln(env.stderr, "host:", err)
		return 1
	}
	for _, name := range site.unmatched {
		fmt.Fprintln(env.stderr, "host: no selected factory for", name)
	}
	if err := site.Start(ctx); err != nil {
		fmt.Fprintln(env.stderr, "host:", site.secrets.redactErr(err))
		if !ownerScoped(err) {
			_ = site.Close(context.Background())
			return 1
		}
		// An owner-scoped recovery failure is reported while the unrelated
		// instances were started, so the host stays operational.
	}
	exit := 0
	if err := site.WaitInitial(ctx); err != nil {
		fmt.Fprintln(env.stderr, "host:", site.secrets.redactErr(err))
		exit = 1
	} else if err := app(ctx, site, task); err != nil {
		fmt.Fprintln(env.stderr, "host:", site.secrets.redactErr(err))
		exit = 1
	}
	if err := site.Close(context.Background()); err != nil {
		fmt.Fprintln(env.stderr, "host:", site.secrets.redactErr(err))
		if exit == 0 {
			exit = 1
		}
	}
	return exit
}

// ownerScoped reports that every error in err is scoped to one instance's
// owner, which the supervisor reports while it starts the unrelated peers.
func ownerScoped(err error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		parts := joined.Unwrap()
		for _, part := range parts {
			if !ownerScoped(part) {
				return false
			}
		}
		return len(parts) > 0
	}
	var scoped *life.SemanticError
	return errors.As(err, &scoped)
}

func loadConfig(env environment, args []string) (Config, error) {
	var path string
	explicit := len(args) > 0
	if explicit {
		path = args[0]
	} else {
		root, err := env.root()
		if err != nil {
			return Config{}, err
		}
		path = filepath.Join(root, "installation.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !explicit && errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("installation file: %w", err)
	}
	return cfg, nil
}

func loadSelection(path string) (*MethodSelection, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var selection MethodSelection
	if err := json.Unmarshal(data, &selection); err != nil {
		return nil, fmt.Errorf("selection file: %w", err)
	}
	return &selection, nil
}

type localIdentity struct {
	InstallationKey string          `json:"installation_key"`
	BridgeID        driver.BridgeID `json:"bridge_id"`
}

const localIdentityFile = "local-identity.json"

// withLocalIdentity completes the configured identity from the durable local
// record below the state directory. The record is generated on first
// initialization and reused exactly afterwards; it depends on no address, host
// name, serial or discovery order. A configured value takes precedence.
func withLocalIdentity(cfg *Config) error {
	if cfg.InstallationKey != "" && cfg.BridgeID != "" {
		return nil
	}
	path := filepath.Join(cfg.StateDir, localIdentityFile)
	var record localIdentity
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &record); err != nil || record.InstallationKey == "" || record.BridgeID == "" {
			return fmt.Errorf("local identity record %s is damaged", localIdentityFile)
		}
	case errors.Is(err, os.ErrNotExist):
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return err
		}
		suffix := hex.EncodeToString(raw[:])
		record = localIdentity{InstallationKey: "installation." + suffix[:16], BridgeID: driver.BridgeID("bridge." + suffix[16:])}
		encoded, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
			return err
		}
		tmp, err := os.CreateTemp(cfg.StateDir, localIdentityFile+".*.tmp")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		_, werr := tmp.Write(append(encoded, '\n'))
		if cerr := tmp.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return werr
		}
		// Linking never replaces a record another open committed first.
		switch lerr := os.Link(tmp.Name(), path); {
		case lerr == nil:
		case errors.Is(lerr, os.ErrExist):
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(data, &record); err != nil || record.InstallationKey == "" || record.BridgeID == "" {
				return fmt.Errorf("local identity record %s is damaged", localIdentityFile)
			}
		default:
			return lerr
		}
	default:
		return err
	}
	if cfg.InstallationKey == "" {
		cfg.InstallationKey = record.InstallationKey
	}
	if cfg.BridgeID == "" {
		cfg.BridgeID = record.BridgeID
	}
	return nil
}

// Site is one open host. It adds no access rule of its own: the host program is
// one calling surface composed in the process.
type Site struct {
	rt        *runtime.Runtime
	selected  []driver.Factory
	unmatched []string
	secrets   *secretScope
}

type secretEntry struct {
	kind      driver.DriverKind
	ref       string
	transport json.RawMessage
	options   json.RawMessage
}

// secretScope holds the configured per-instance secret references so they reach
// only the factory that plans their instance and never appear in diagnostics.
type secretScope struct {
	refs      map[string][]secretEntry
	factories []driver.Factory
}

func (s *secretScope) redact(text string) string {
	for _, entries := range s.refs {
		for _, entry := range entries {
			text = strings.ReplaceAll(text, entry.ref, "[secret reference]")
		}
	}
	return text
}

func (s *secretScope) redactErr(err error) error {
	if err == nil {
		return nil
	}
	if text := s.redact(err.Error()); text != err.Error() {
		return errors.New(text)
	}
	return err
}

// secretScopedFactory plans an instance without its secret reference first and
// hands the reference only when that plan names the instance's own kind.
type secretScopedFactory struct {
	driver.Factory
	scope *secretScope
	index int
}

func (f *secretScopedFactory) Plan(config driver.Config) ([]driver.InstancePlan, error) {
	if config.SecretRef == "" {
		return f.Factory.Plan(config)
	}
	// The record being planned is the one carrying exactly these operands and
	// this reference; its kind, not another record's of the same instance key,
	// decides whether this factory owns the reference.
	kinds := map[driver.DriverKind]bool{}
	for _, entry := range f.scope.refs[config.Instance] {
		if entry.ref == config.SecretRef && bytes.Equal(entry.transport, config.Transport) && bytes.Equal(entry.options, config.Options) {
			kinds[entry.kind] = true
		}
	}
	if len(kinds) == 0 {
		return f.Factory.Plan(config)
	}
	owns := func(plans []driver.InstancePlan) bool {
		for _, plan := range plans {
			if kinds[plan.Driver] {
				return true
			}
		}
		return false
	}
	// Which factory owns the record is learned from the record's own operands
	// without any reference: a factory that plans the record's kind from them
	// owns it and is asked again with the real reference, whose answer is
	// authoritative. A factory that plans another kind never receives the
	// reference. A factory that cannot plan the record without a reference is
	// its candidate owner only when no other selected factory plans the kind.
	probe := config
	probe.SecretRef = ""
	plans, err := f.Factory.Plan(probe)
	if err == nil && !owns(plans) {
		return plans, nil
	}
	if err != nil {
		// A factory that fails without the reference is the owner only when
		// every other selected factory plans the record without error and
		// claims none of its kinds; another failure leaves the owner unknown
		// and the reference is then sent to nobody.
		for i, other := range f.scope.factories {
			if i == f.index {
				continue
			}
			if others, oerr := other.Plan(probe); oerr != nil || owns(others) {
				return plans, f.scope.redactErr(err)
			}
		}
	}
	plans, err = f.Factory.Plan(config)
	return plans, f.scope.redactErr(err)
}

// Open maps the configuration onto the installation and opens the runtime. It
// constructs no instance: Start does. An enabled instance whose kind matches no
// selected factory is left out and reported by Unmatched.
func Open(ctx context.Context, cfg Config, selected ...driver.Factory) (*Site, error) {
	return open(ctx, defaultEnvironment(), cfg, selected)
}

func open(ctx context.Context, env environment, cfg Config, selected []driver.Factory) (*Site, error) {
	if cfg.StateDir == "" {
		root, err := env.root()
		if err != nil {
			return nil, err
		}
		cfg.StateDir = root
	}
	if err := withLocalIdentity(&cfg); err != nil {
		return nil, err
	}
	inst := life.Installation{
		Key: cfg.InstallationKey, Bridge: cfg.BridgeID, StateDir: cfg.StateDir, Clock: cfg.Clock, Calendar: cfg.Calendar,
	}
	var unmatched []string
	scoped := &secretScope{refs: map[string][]secretEntry{}, factories: selected}
	for _, ic := range cfg.Instances {
		if ic.SecretRef != "" {
			scoped.refs[string(ic.Instance)] = append(scoped.refs[string(ic.Instance)], secretEntry{kind: ic.Kind, ref: ic.SecretRef, transport: ic.Transport, options: ic.Options})
		}
	}
	for _, ic := range cfg.Instances {
		config := driver.Config{
			Instance: string(ic.Instance), Enabled: ic.Enabled,
			Transport: append(json.RawMessage(nil), ic.Transport...), Options: append(json.RawMessage(nil), ic.Options...),
			SecretRef: ic.SecretRef,
		}
		if ic.Enabled && !matches(selected, ic.Kind, config) {
			unmatched = append(unmatched, string(ic.Kind)+"/"+string(ic.Instance))
			continue
		}
		inst.Instances = append(inst.Instances, life.InstanceConfig{Driver: ic.Kind, Instance: ic.Instance, Enabled: ic.Enabled, Config: config})
	}
	wrapped := make([]driver.Factory, len(selected))
	for i, factory := range selected {
		wrapped[i] = &secretScopedFactory{Factory: factory, scope: scoped, index: i}
	}
	rt, err := runtime.OpenHost(ctx, inst, runtime.HostOptions{Statements: cfg.OperatorIdentityStatements}, wrapped...)
	if err != nil {
		return nil, scoped.redactErr(err)
	}
	return &Site{rt: rt, selected: selected, unmatched: unmatched, secrets: scoped}, nil
}

// matches reports whether a selected factory plans the kind for the
// configuration. A factory that fails to plan counts as a match, so the planning
// error is reported by the runtime.
func matches(selected []driver.Factory, kind driver.DriverKind, config driver.Config) bool {
	for _, factory := range selected {
		config.SecretRef = ""
		plans, err := factory.Plan(config)
		if err != nil {
			return true
		}
		for _, plan := range plans {
			if plan.Driver == kind {
				return true
			}
		}
	}
	return false
}

// Unmatched names the enabled instances whose kind matches no selected factory,
// as kind/instance.
func (s *Site) Unmatched() []string { return append([]string(nil), s.unmatched...) }

// Start restores committed publication and starts the instances.
func (s *Site) Start(ctx context.Context) error { return s.rt.Start(ctx) }

// WaitInitial waits for the first publications.
func (s *Site) WaitInitial(ctx context.Context) error { return s.rt.WaitInitial(ctx) }

// Close stops the instances and closes the runtime.
func (s *Site) Close(ctx context.Context) error { return s.rt.Close(ctx) }

// Runtime is the runtime the site wraps.
func (s *Site) Runtime() *runtime.Runtime { return s.rt }

// Tree returns SS09's tree: Capture, Resume, Prepare, Call and Outcome.
func (s *Site) Tree() *tree.Tree { return s.rt.Tree() }

// Invoke calls a prepared method once.
func (s *Site) Invoke(ctx context.Context, prepared tree.Prepared) (tree.Receipt, error) {
	return s.rt.Invoke(ctx, prepared)
}

// WaitOutcome returns the outcome of the receipt Invoke returned.
func (s *Site) WaitOutcome(ctx context.Context, id tree.ReceiptID) (tree.Outcome, error) {
	return s.rt.Outcome(ctx, id)
}

// SetDesired passes the supervisor's decision through unchanged.
func (s *Site) SetDesired(ctx context.Context, ref life.Ref, method life.Method, precondition life.DesiredRevision) (life.DesiredView, error) {
	return s.rt.SetDesired(ctx, ref, method, precondition)
}

// Snapshot passes the supervisor's observed state of one instance through
// unchanged.
func (s *Site) Snapshot(ref life.Ref) (life.Status, error) { return s.rt.Snapshot(ref) }

// Watch passes the supervisor's observed state and its change channel through
// unchanged.
func (s *Site) Watch(ref life.Ref) (life.Status, <-chan struct{}, error) { return s.rt.Watch(ref) }

// Families returns each selected factory's declared family records.
func (s *Site) Families() []catalog.Family {
	var out []catalog.Family
	for _, factory := range s.selected {
		out = append(out, factory.Descriptor().Families...)
	}
	return out
}
