/*
Copyright © 2019 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cmd

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/containers/toolbox/pkg/podman"
	"github.com/containers/toolbox/pkg/version"

	"github.com/containers/toolbox/pkg/utils"
	"github.com/spf13/cobra"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

var (
	cfgFile   string
	rootFlags struct {
		loglevel  string
		logPodman bool
		assumeyes bool
		verbose   bool
	}
	rootCmd = &cobra.Command{
		Use:   "toolbox",
		Short: "Unprivileged development environment",
		Long: `Toolbox is a tool that offers a familiar RPM based environment for
developing and debugging software that runs fully unprivileged using Podman.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// This sets up loggers for all commands
			err := setUpLoggers()
			if err != nil {
				return err
			}

			// Resolve the path to the toolbox binary
			toolboxCmdPath, err := os.Executable()
			if err != nil {
				logrus.Fatalf("Failed to get absolute path to %s", os.Args[0])
			}
			viper.Set("TOOLBOX_CMD_PATH", toolboxCmdPath)
			logrus.Debugf("Absolute path to %s is %s", os.Args[0], viper.Get("TOOLBOX_CMD_PATH"))

			// Find out if the TOOLBOX_PATH env var is set
			toolboxPath := viper.GetString("TOOLBOX_PATH")
			calledCmd := cmd.CalledAs()
			inContainer := utils.PathExists("/run/.containerenv")

			if inContainer {
				if toolboxPath == "" {
					logrus.Fatal("TOOLBOX_PATH is not set")
				}
				if calledCmd != "init-container" {
					logrus.Fatal("Toolbox currently does not work inside of a container. Please, run it on the host.")
				}
			} else {
				if toolboxPath == "" {
					viper.Set("TOOLBOX_PATH", viper.GetString("TOOLBOX_CMD_PATH"))
				}
			}
			logrus.Debugf("TOOLBOX_PATH is %s", viper.GetString("TOOLBOX_PATH"))

			// Set the toolbox runtime directory
			viper.Set("TOOLBOX_RUNTIME_DIRECTORY", fmt.Sprintf("%s/toolbox", viper.GetString("XDG_RUNTIME_DIR")))
			logrus.Debugf("Toolbox runtime directory is %s", viper.GetString("TOOLBOX_RUNTIME_DIRECTORY"))

			// Check if it is needed to migrate to a new Podman version
			// This doesn't have to be done in a container
			if calledCmd != "init-container" && !inContainer {
				err = migrate()
				if err != nil {
					logrus.Fatal(err)
				}
			}

			// Here we could place some logic to take care of invoing toolbox or other commands from within container by piping them to the host
			// TODO

			return nil
		},
		Version:       version.GetVersion(),
		SilenceErrors: true,
	}
)

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().BoolVarP(&rootFlags.assumeyes, "assumeyes", "y", false, "Automatically answer yes for all questions")
	rootCmd.PersistentFlags().StringVar(&rootFlags.loglevel, "log-level", "warn", "Log messages above specified level: trace, debug, info, warn, error, fatal or panic")
	rootCmd.PersistentFlags().BoolVar(&rootFlags.logPodman, "log-podman", false, "Show the log output of Podman. The log level is handled by the log-level option")
	viper.BindPFlag("log-podman", rootCmd.PersistentFlags().Lookup("log-podman"))
	// This flag is kept for compatibility reasons. In the future it would be better removed.
	rootCmd.PersistentFlags().BoolVar(&rootFlags.verbose, "verbose", false, "Set log-level to 'debug'")
	rootCmd.PersistentFlags().MarkDeprecated("verbose", "use 'log-level' instead.")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		// Search config in home directory with name ".toolbox" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigName(".toolbox")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}

func setUpLoggers() error {
	logrus.SetOutput(os.Stderr)
	logrus.SetFormatter(&logrus.TextFormatter{
		DisableTimestamp:       true,
		DisableLevelTruncation: true,
	})

	if rootFlags.verbose {
		rootFlags.loglevel = "debug"
	}

	lvl, err := logrus.ParseLevel(rootFlags.loglevel)
	if err != nil {
		return err
	}

	logrus.SetLevel(lvl)

	return nil
}

func migrate() error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("Could not get the user config directory: %w", err)
	}
	toolboxConfigDir := fmt.Sprintf("%s/toolbox", configDir)
	migrateStampPath := fmt.Sprintf("%s/podman-system-migrate", configDir)
	logrus.Debugf("Toolbox config directory is %s", toolboxConfigDir)

	podmanVersion, err := podman.GetVersion()
	if err != nil {
		return fmt.Errorf("Could not get the version of Podman: %w", err)
	}
	logrus.Debugf("Current Podman version is %s", podmanVersion)

	err = os.MkdirAll(toolboxConfigDir, 0664)
	if err != nil {
		return fmt.Errorf("Configuration directory not created: %w", err)
	}

	output, err := ioutil.ReadFile(migrateStampPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("Could not read file '%s': %w", migrateStampPath, err)
		}
	}
	podmanVersionOld := string(output)

	if podmanVersionOld != "" {
		logrus.Debugf("Old Podman version is %s", podmanVersionOld)

		if podmanVersion == podmanVersionOld {
			logrus.Debugf("Migration not needed: Podman version %s is unchanged", podmanVersion)
			return nil
		}

		if !podman.CheckVersion(podmanVersionOld) {
			logrus.Debugf("Migration not needed: Podman version %s is old", podmanVersion)
			return nil
		}
	}

	logrus.Debugf("Migration needed: Podman version %s is new", podmanVersion)
	err = podman.CmdRun("system", "migrate")
	if err != nil {
		return fmt.Errorf("Unable to migrate containers: %w", err)
	}
	logrus.Debugf("Migration to Podman version %s was ok", podmanVersion)

	logrus.Infof("Updating Podman version in '%s'", migrateStampPath)
	err = ioutil.WriteFile(migrateStampPath, []byte(podmanVersion), 0664)
	if err != nil {
		return fmt.Errorf("Could not update version of Podman in '%s': %w", migrateStampPath, err)
	}

	return nil
}