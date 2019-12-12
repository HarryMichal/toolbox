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
	"fmt"
	"os"

	"github.com/spf13/cobra"

	homedir "github.com/mitchellh/go-homedir"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

var (
	cfgFile   string
	rootFlags struct {
		loglevel  string
		assumeyes bool
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

			// Here we could place some logic to take care of invoing toolbox or other commands from within container by piping them to the host
			// FIXME

			return nil
		},
	}
)

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&rootFlags.loglevel, "log-level", "error", "Log messages above specified level: trace, debug, info, warn, error, fatal or panic")
	rootCmd.Flags().BoolVarP(&rootFlags.assumeyes, "assumeyes", "y", false, "Automatically answer yes for all questions.")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := homedir.Dir()
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
	logrus.SetOutput(os.Stdout)

	lvl, err := logrus.ParseLevel(rootFlags.loglevel)
	if err != nil {
		return err
	}

	logrus.SetLevel(lvl)

	return nil
}
