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

// WriteDiagnostics saves into dir what the signer and its validator client
// report about the signing: the signer's metrics, its healthcheck and
// capabilities as it serves them to the client's token, which is never
// written, and the validator client's remote signer metrics. It saves every
// part it can read; the parts it cannot are listed in errors.txt, and
// returned.
func WriteDiagnostics(ctx context.Context, dir string, client *Client, metricsURL, validatorMetricsURL string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	var problems []error
	save := func(name string, body []byte, err error) {
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, name), body, 0o600)
		}
		if err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", name, err))
		}
	}

	scrape, err := ScrapeText(ctx, metricsURL)
	save(DiagnosticsMetricsFile, scrape, err)
	for _, part := range []struct{ name, path string }{
		{DiagnosticsHealthcheckFile, healthcheckPath},
		{DiagnosticsCapabilitiesFile, capabilitiesPath},
	} {
		status, body, err := client.Raw(ctx, part.path)
		if err == nil && status != http.StatusOK {
			// The body is saved anyway: an unhealthy signer says why.
			problems = append(problems, fmt.Errorf("%s: GET %s returned %d", part.name, part.path, status))
		}
		save(part.name, body, err)
	}
	if validatorMetricsURL == "" {
		save(DiagnosticsValidatorMetricsFile, nil, errors.New("the validator client publishes no metrics port"))
	} else {
		scrape, err := ScrapeText(ctx, validatorMetricsURL)
		save(DiagnosticsValidatorMetricsFile, MetricLines(scrape, ValidatorMetricPrefix), err)
	}

	if len(problems) == 0 {
		return nil
	}
	lines := make([]string, len(problems))
	for index, problem := range problems {
		lines[index] = problem.Error()
	}
	if err := os.WriteFile(filepath.Join(dir, DiagnosticsErrorsFile), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		problems = append(problems, err)
	}
	return errors.Join(problems...)
}
