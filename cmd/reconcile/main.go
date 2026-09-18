// Command reconcile is the CLI entrypoint: it reconciles a PSP settlement CSV
// against an internal ledger CSV and prints a discrepancy report.
//
// Usage:
//
//	reconcile --psp psp.csv --ledger ledger.csv [--format json|text] \
//	          [--out report.json] [--high 10000] [--critical 100000] \
//	          [--fail-on-discrepancy]
//
// Exit codes:
//
//	0  success, no discrepancies (or --fail-on-discrepancy not set)
//	1  usage or runtime error (bad flags, unreadable/invalid input)
//	2  ran successfully but discrepancies were found (--fail-on-discrepancy)
//
// The distinct code 2 lets a nightly cron or CI job gate on "did anything fail
// to reconcile?" without conflating it with a real error.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/jatin-gl/payment-reconciliation-engine/internal/contract"
	"github.com/jatin-gl/payment-reconciliation-engine/internal/ingest"
	"github.com/jatin-gl/payment-reconciliation-engine/internal/matcher"
	"github.com/jatin-gl/payment-reconciliation-engine/internal/model"
	"github.com/jatin-gl/payment-reconciliation-engine/internal/recon"
)

// errDiscrepancies is returned by run when --fail-on-discrepancy is set and the
// report contains at least one finding. main maps it to exit code 2.
var errDiscrepancies = errors.New("discrepancies found")

func main() {
	err := run(os.Args[1:], os.Stdout)
	switch {
	case err == nil:
		// success
	case errors.Is(err, errDiscrepancies):
		os.Exit(2)
	default:
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	fs.SetOutput(stdout)
	pspPath := fs.String("psp", "", "path to the PSP settlement CSV (required)")
	ledgerPath := fs.String("ledger", "", "path to the internal ledger CSV (required)")
	format := fs.String("format", "text", "output format: text | json")
	outPath := fs.String("out", "", "write output to this file instead of stdout")
	high := fs.Int64("high", matcher.DefaultConfig().HighImpactMinor, "high-impact threshold in minor units")
	critical := fs.Int64("critical", matcher.DefaultConfig().CriticalImpactMinor, "critical-impact threshold in minor units")
	failOnDisc := fs.Bool("fail-on-discrepancy", false, "exit with code 2 if any discrepancy is found")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil // -h/--help already printed usage; exit 0 by convention
		}
		return err
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("unknown format %q (want text or json)", *format)
	}
	if *pspPath == "" || *ledgerPath == "" {
		fs.Usage()
		return fmt.Errorf("--psp and --ledger are required")
	}

	psp, err := parseFile(*pspPath, model.SourcePSP, ingest.DefaultPSPColumns())
	if err != nil {
		return fmt.Errorf("psp: %w", err)
	}
	ledger, err := parseFile(*ledgerPath, model.SourceLedger, ingest.DefaultLedgerColumns())
	if err != nil {
		return fmt.Errorf("ledger: %w", err)
	}

	engine := recon.New(matcher.Config{HighImpactMinor: *high, CriticalImpactMinor: *critical})
	report := engine.Reconcile(psp, ledger)

	out := stdout
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			return err
		}
		defer f.Close()
		out = f
	}

	if err := writeReport(out, report, *format); err != nil {
		return err
	}

	if *failOnDisc && report.Summary.DiscrepancyCount > 0 {
		return errDiscrepancies
	}
	return nil
}

func writeReport(out io.Writer, report model.Report, format string) error {
	switch format {
	case "json":
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(contract.FromReport(report))
	default: // "text" — validated by the caller
		return writeText(out, report)
	}
}

func parseFile(path string, source model.Source, cm ingest.ColumnMap) ([]model.Transaction, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ingest.ParseCSV(f, source, cm, ingest.DefaultStatusMap())
}

func writeText(out io.Writer, r model.Report) error {
	fmt.Fprintf(out, "Reconciliation report %s (contract v%s)\n", r.ReportID, r.ContractVersion)
	fmt.Fprintf(out, "Generated: %s\n\n", r.GeneratedAt.Format("2006-01-02 15:04:05 MST"))
	fmt.Fprintf(out, "  PSP records:      %d\n", r.Summary.PSPCount)
	fmt.Fprintf(out, "  Ledger records:   %d\n", r.Summary.LedgerCount)
	fmt.Fprintf(out, "  Cleanly matched:  %d\n", r.Summary.MatchedCount)
	fmt.Fprintf(out, "  Discrepancies:    %d\n", r.Summary.DiscrepancyCount)
	fmt.Fprintf(out, "  Money at risk:    %s\n", r.Summary.TotalMonetaryImpact)
	if r.Summary.DiscrepancyCount == 0 {
		fmt.Fprintln(out, "\n✓ fully reconciled")
		return nil
	}
	fmt.Fprintln(out, "\nFindings (most severe first):")
	for _, d := range r.Discrepancies {
		fmt.Fprintf(out, "  [%-8s] %-18s %-16s %s\n", d.Severity, d.Type, d.MatchKey, d.Detail)
	}
	return nil
}
