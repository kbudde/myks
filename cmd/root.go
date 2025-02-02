package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	aurora "github.com/logrusorgru/aurora/v4"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/mykso/myks/internal/myks"
)

const (
	ANNOTATION_SMART_MODE = "feat:smart-mode"
	ANNOTATION_TRUE       = "true"
	MYKS_CONFIG_NAME      = ".myks"
	MYKS_CONFIG_TYPE      = "yaml"
)

var (
	cfgFile    string
	envAppMap  myks.EnvAppMap
	asyncLevel int
)

var rootCmd = &cobra.Command{
	Use:   "myks",
	Short: "Myks helps to manage configuration for kubernetes clusters",
	Long: `Myks fetches K8s workloads from a variety of sources, e.g. Helm charts or Git Repositories. It renders their respective yaml files to the file system in a structure of environments and their applications.

It supports prototype applications that can be shared between environments and inheritance of configuration from parent environments to their "children".

Myks supports two positional arguments:

- A comma-separated list of environments to render. If you provide "ALL", all environments will be rendered.
- A comma-separated list of applications to render. If you don't provide this argument or provide "ALL", all applications will be rendered.

If you do not provide any positional arguments, myks will run in "Smart Mode". In Smart Mode, myks will only render environments and applications that have changed since the last run.
`,
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringP("log-level", "l", "info", "set the logging level")

	asyncHelp := "sets the number of applications to be processed in parallel\nthe default (0) is no limit"
	rootCmd.PersistentFlags().IntVarP(&asyncLevel, "async", "a", 0, asyncHelp)

	smartModeBaseRevisionHelp := "base revision to compare against in Smart Mode\n" +
		"if not provided, only local changes will be considered"
	rootCmd.PersistentFlags().String("smart-mode.base-revision", "", smartModeBaseRevisionHelp)

	smartModeOnlyPrintHelp := "only print the list of environments and applications that would be rendered in Smart Mode"
	rootCmd.PersistentFlags().Bool("smart-mode.only-print", false, smartModeOnlyPrintHelp)

	if err := viper.BindPFlags(rootCmd.PersistentFlags()); err != nil {
		log.Fatal().Err(err).Msg("Unable to bind flags")
	}
	if err := viper.BindPFlags(rootCmd.Flags()); err != nil {
		log.Fatal().Err(err).Msg("Unable to bind flags")
	}

	configHelp := fmt.Sprintf("config file (default is the first %s.%s up the directory tree)", MYKS_CONFIG_NAME, MYKS_CONFIG_TYPE)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", configHelp)

	rootCmd.PersistentPreRunE = detectTargetEnvsAndApps
}

func detectTargetEnvsAndApps(cmd *cobra.Command, args []string) (err error) {
	// Check positional arguments for Smart Mode:
	// 1. Comma-separated list of environment search paths or ALL to search everywhere (default: ALL)
	// 2. Comma-separated list of application names or none to process all applications (default: none)

	if cmd.Annotations[ANNOTATION_SMART_MODE] != ANNOTATION_TRUE {
		log.Debug().Msg("Smart Mode is not supported for this command.")
		return
	}

	switch len(args) {
	case 0:
		// smart mode requires instantiation of globe object to get the list of environments
		// the globe object will not be used later in the process. It is only used to get the list of all environments and their apps.
		globeAllEnvsAndApps := myks.New(".")
		envAppMap, err = globeAllEnvsAndApps.DetectChangedEnvsAndApps(viper.GetString("smart-mode.base-revision"))
		if err != nil {
			log.Warn().Err(err).Msg("Unable to run Smart Mode. Rendering everything.")
		} else if len(envAppMap) == 0 {
			log.Warn().Msg("Smart Mode did not find any changes. Exiting.")
			os.Exit(0)
		}
	case 1:
		if args[0] != "ALL" {
			envAppMap = make(myks.EnvAppMap)
			for _, env := range strings.Split(args[0], ",") {
				envAppMap[env] = nil
			}
		}
	case 2:
		var appNames []string
		if args[1] != "ALL" {
			appNames = strings.Split(args[1], ",")
		}

		envAppMap = make(myks.EnvAppMap)
		if args[0] != "ALL" {
			for _, env := range strings.Split(args[0], ",") {
				envAppMap[env] = appNames
			}
		} else {
			// TODO: Use Globe.EnvironmentBaseDir instead of the hardcoded key
			envAppMap["envs"] = appNames
		}

	default:
		err := errors.New("Too many positional arguments")
		log.Error().Err(err).Msg("Unable to parse positional arguments")
		return err
	}

	log.Debug().
		Interface("envAppMap", envAppMap).
		Msg("Parsed arguments")

	if viper.GetBool("smart-mode.only-print") {
		fmt.Println(aurora.Bold("\nSmart Mode detected:"))
		for env, apps := range envAppMap {
			fmt.Printf("→ %s\n", env)
			if apps == nil {
				fmt.Println(aurora.Bold("    ALL"))
			} else {
				for _, app := range apps {
					fmt.Printf("    %s\n", app)
				}
			}
		}
		os.Exit(0)
	}

	return nil
}

func initConfig() {
	viper.SetEnvPrefix("MYKS")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	viper.AutomaticEnv()

	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.AddConfigPath(".")
		viper.SetConfigName(MYKS_CONFIG_NAME)
		viper.SetConfigType(MYKS_CONFIG_TYPE)

		// Add all parent directories to the config search path
		dir, _ := os.Getwd()
		for {
			dir = filepath.Dir(dir)
			viper.AddConfigPath(dir)
			if dir == "/" || dir == filepath.Dir(dir) {
				break
			}
		}
	}

	err := viper.ReadInConfig()
	initLogger()

	if err == nil {
		// TODO: Make paths in config file relative to the config file
		log.Info().Msgf("Using config file: %s", viper.ConfigFileUsed())
	} else {
		log.Debug().Err(err).Msg("Unable to read config file")
	}
}

func initLogger() {
	zerolog.DurationFieldUnit = time.Millisecond
	zerolog.DurationFieldInteger = true

	logLevel, err := zerolog.ParseLevel(viper.GetString("log-level"))
	if err != nil {
		log.Fatal().Err(err).Msg("Unable to parse log level")
	}

	log.Info().Msgf("Setting log level to: %s", logLevel)
	zerolog.SetGlobalLevel(logLevel)
}

func SetVersionInfo(version, commit, date string) {
	rootCmd.Version = fmt.Sprintf(`%s
     commit: %s
     date:   %s`, version, commit, date)
}

func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}
