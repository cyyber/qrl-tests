package remotesigner

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// The files WriteDiagnostics writes.
const (
	DiagnosticsMetricsFile          = "metrics.txt"
	DiagnosticsHealthcheckFile      = "healthcheck.json"
	DiagnosticsCapabilitiesFile     = "capabilities.json"
	DiagnosticsValidatorMetricsFile = "validator-metrics.txt"
	DiagnosticsErrorsFile           = "errors.txt"
)

// DiagnosticsAuditLogFile is the audit log WriteExecutionDiagnostics saves.
const DiagnosticsAuditLogFile = "audit.jsonl"

// WriteDiagnostics saves into dir what the signer and its validator client
// report about the signing: the signer's metrics, its healthcheck and
// capabilities as it serves them to the client's token, which is never
// written, and the validator client's remote signer metrics. It saves every
// part it can read; the parts it cannot are listed in errors.txt, and
// returned.
func WriteDiagnostics(ctx context.Context, dir string, client *Client, metricsURL, validatorMetricsURL string) error {
	diagnostics, err := newDiagnostics(dir)
	if err != nil {
		return err
	}
	diagnostics.saveSigner(ctx, client, metricsURL, healthcheckPath, capabilitiesPath)
	if validatorMetricsURL == "" {
		diagnostics.save(DiagnosticsValidatorMetricsFile, nil, errors.New("the validator client publishes no metrics port"))
	} else {
		scrape, err := ScrapeText(ctx, validatorMetricsURL)
		diagnostics.save(DiagnosticsValidatorMetricsFile, MetricLines(scrape, ValidatorMetricPrefix), err)
	}
	return diagnostics.finish()
}

// WriteExecutionDiagnostics saves into dir what the signer reports in
// execution mode: its metrics, its healthcheck as it serves it to the
// client's token, which is never written, and the audit log readAuditLog
// returns. As WriteDiagnostics does, it saves every part it can read and
// lists the others in errors.txt.
func WriteExecutionDiagnostics(ctx context.Context, dir string, client *Client, metricsURL string,
	readAuditLog func(context.Context) ([]byte, error)) error {
	diagnostics, err := newDiagnostics(dir)
	if err != nil {
		return err
	}
	diagnostics.saveSigner(ctx, client, metricsURL, healthcheckPath)
	log, err := readAuditLog(ctx)
	diagnostics.save(DiagnosticsAuditLogFile, log, err)
	return diagnostics.finish()
}

type diagnostics struct {
	dir      string
	problems []error
}

func newDiagnostics(dir string) (*diagnostics, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &diagnostics{dir: dir}, nil
}

func (diagnostics *diagnostics) save(name string, body []byte, err error) {
	if err == nil {
		err = os.WriteFile(filepath.Join(diagnostics.dir, name), body, 0o600)
	}
	if err != nil {
		diagnostics.problems = append(diagnostics.problems, fmt.Errorf("%s: %w", name, err))
	}
}

// saveSigner saves the signer's metrics and the answers to GETs of paths,
// each in the file its path names.
func (diagnostics *diagnostics) saveSigner(ctx context.Context, client *Client, metricsURL string, paths ...string) {
	scrape, err := ScrapeText(ctx, metricsURL)
	diagnostics.save(DiagnosticsMetricsFile, scrape, err)
	names := map[string]string{
		healthcheckPath:  DiagnosticsHealthcheckFile,
		capabilitiesPath: DiagnosticsCapabilitiesFile,
	}
	for _, path := range paths {
		name := names[path]
		status, body, err := client.Raw(ctx, path)
		if err == nil && status != http.StatusOK {
			// The body is saved anyway: an unhealthy signer says why.
			diagnostics.problems = append(diagnostics.problems, fmt.Errorf("%s: GET %s returned %d", name, path, status))
		}
		diagnostics.save(name, body, err)
	}
}

// finish lists the parts it could not save in errors.txt, and returns them.
func (diagnostics *diagnostics) finish() error {
	if len(diagnostics.problems) == 0 {
		return nil
	}
	lines := make([]string, len(diagnostics.problems))
	for index, problem := range diagnostics.problems {
		lines[index] = problem.Error()
	}
	errorsFile := filepath.Join(diagnostics.dir, DiagnosticsErrorsFile)
	if err := os.WriteFile(errorsFile, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		diagnostics.problems = append(diagnostics.problems, err)
	}
	return errors.Join(diagnostics.problems...)
}
