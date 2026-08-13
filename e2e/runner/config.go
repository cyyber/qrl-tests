package runner

import "github.com/cyyber/qrl-tests/devnet"

func (configuration Config) withDefaults() Config {
	if configuration.TestsDir == "" {
		configuration.TestsDir = "."
	}
	if configuration.BaseName == "" {
		configuration.BaseName = devnet.DefaultEnclaveName
	}
	if configuration.ReportDir == "" {
		configuration.ReportDir = DefaultReportDir
	}
	if configuration.Backend == "" {
		configuration.Backend = devnet.BackendDocker
	}
	if configuration.StartTimeout == 0 {
		configuration.StartTimeout = devnet.DefaultStartTimeout
	}
	return configuration
}
