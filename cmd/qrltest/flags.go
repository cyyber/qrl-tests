package main

import (
	"fmt"
	"os"

	"github.com/cyyber/qrl-tests/devnet"
	"github.com/urfave/cli/v2"
)

func enclaveNameFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:    "enclave-name",
		Usage:   "Kurtosis enclave name",
		Value:   devnet.DefaultEnclaveName,
		EnvVars: []string{"DEVNET_ENCLAVE_NAME"},
	}
}

func backendFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:    "backend",
		Usage:   "Kurtosis backend: docker or kubernetes",
		Value:   string(devnet.BackendDocker),
		EnvVars: []string{"DEVNET_BACKEND"},
	}
}

func packageLocatorFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:    "qrl-package",
		Usage:   "qrl-package locator: github.com/<owner>/<repository>[@<ref>]",
		Value:   devnet.DefaultPackageLocator,
		EnvVars: []string{"DEVNET_QRL_PACKAGE_REF"},
	}
}

func parametersFileFlag() *cli.PathFlag {
	return &cli.PathFlag{
		Name:    "params-file",
		Usage:   "complete YAML or JSON qrl-package parameters",
		EnvVars: []string{"DEVNET_PARAMS_FILE"},
	}
}

func readParametersFile(command *cli.Context) ([]byte, error) {
	file := command.Path("params-file")
	if file == "" {
		return nil, nil
	}
	payload, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read parameters file: %w", err)
	}
	return payload, nil
}

func imageFlags() []cli.Flag {
	images := devnet.DefaultImages()
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "execution-image",
			Usage:   "execution image reference",
			Value:   images.Execution,
			EnvVars: []string{"DEVNET_EXECUTION_IMAGE"},
		},
		&cli.StringFlag{
			Name:    "clef-image",
			Usage:   "Clef image reference",
			Value:   images.Clef,
			EnvVars: []string{"DEVNET_CLEF_IMAGE"},
		},
		&cli.StringFlag{
			Name:    "consensus-image",
			Usage:   "consensus client image reference",
			Value:   images.Consensus,
			EnvVars: []string{"DEVNET_CONSENSUS_IMAGE"},
		},
		&cli.StringFlag{
			Name:    "validator-image",
			Usage:   "validator client image reference",
			Value:   images.Validator,
			EnvVars: []string{"DEVNET_VALIDATOR_IMAGE"},
		},
		&cli.StringFlag{
			Name:    "genesis-image",
			Usage:   "genesis generator image reference",
			Value:   images.Genesis,
			EnvVars: []string{"DEVNET_GENESIS_IMAGE"},
		},
	}
}

func imagesFromFlags(command *cli.Context) devnet.Images {
	return devnet.Images{
		Execution: command.String("execution-image"),
		Clef:      command.String("clef-image"),
		Consensus: command.String("consensus-image"),
		Validator: command.String("validator-image"),
		Genesis:   command.String("genesis-image"),
	}
}
