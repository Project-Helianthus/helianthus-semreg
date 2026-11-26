// Package host is the ready-to-run entry point of any host program built on
// SemReg. A program that imports this package and the driver factories it
// chooses is a complete host: one call plans and starts the instances, waits for
// their first publications, hands the program a Site from which it captures the
// whole installation, walks every node, value and method, runs an operation and
// waits for its outcome, and closes everything at the end. The package imports
// no concrete driver.
//
// A host program:
//
//	package main
//
//	import (
//		"context"
//		"os"
//
//		"github.com/Project-Helianthus/helianthus-semreg/semreg/host"
//		"example.org/mydriver"
//	)
//
//	func main() {
//		os.Exit(host.Main(os.Args, run, mydriver.Factory{}))
//	}
//
//	func run(ctx context.Context, site *host.Site, task host.Task) error {
//		cut, err := site.Cut(ctx)
//		if err != nil {
//			return err
//		}
//		defer cut.Close()
//		if _, err := cut.Walk(ctx, host.AllVisible(100), func(host.WalkEntry) error { return nil }); err != nil {
//			return err
//		}
//		if task.Selection == nil {
//			return nil
//		}
//		prepared, err := cut.Prepare(ctx, *task.Selection)
//		if err != nil {
//			return err
//		}
//		receipt, err := site.Invoke(ctx, prepared)
//		if err != nil {
//			return err
//		}
//		_, err = site.WaitOutcome(ctx, receipt.ID)
//		return err
//	}
//
// Main takes the positional inputs `<program> [installation.json
// [selection.json|-]]`. A host that parses its own flags calls DispatchEarly
// first and then Open.
package host
