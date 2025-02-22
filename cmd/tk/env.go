package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/grafana/tanka/pkg/spec/v1alpha1"
	"github.com/grafana/tanka/pkg/util"
)

func envCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env [action]",
		Short: "manipulate environments",
	}
	cmd.PersistentFlags().Bool("json", false, "output in json format")
	cmd.AddCommand(
		envAddCmd(),
		envSetCmd(),
		envListCmd(),
		envRemoveCmd(),
	)
	return cmd
}

func envSettingsFlags(env *v1alpha1.Config, fs *pflag.FlagSet) {
	fs.StringVar(&env.Spec.APIServer, "server", env.Spec.APIServer, "endpoint of the Kubernetes API")
	fs.StringVar(&env.Spec.Namespace, "namespace", env.Spec.Namespace, "namespace to create objects in")
	fs.StringVar(&env.Spec.DiffStrategy, "diff-strategy", env.Spec.DiffStrategy, "specify diff-strategy. Automatically detected otherwise.")
}

func envSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set",
		Short: "update properties of an environment",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"args": "baseDir",
		},
	}
	tmp := v1alpha1.Config{}
	envSettingsFlags(&tmp, cmd.Flags())

	name := cmd.Flags().String("name", "", "")
	cmd.Flags().MarkHidden("name")

	cmd.Run = func(cmd *cobra.Command, args []string) {
		if *name != "" {
			log.Fatalln("It looks like you attempted to rename the environment using `--name`. However, this is not possible with Tanka, because the environments name is inferred from the directories name. To rename the environment, rename its directory instead.")
		}

		path, err := filepath.Abs(args[0])
		if err != nil {
			log.Fatalln(err)
		}

		viper.Reset()
		cfg := setupConfiguration(path)
		if cfg == nil {
			log.Fatalf("Failed to load an environment at `%s`.\nMake sure it exists and is properly configured. See https://tanka.dev/environments/ for details.", path)
		}
		if tmp.Spec.APIServer != "" && tmp.Spec.APIServer != cfg.Spec.APIServer {
			fmt.Printf("updated spec.apiServer (`%s -> `%s`)\n", cfg.Spec.APIServer, tmp.Spec.APIServer)
			cfg.Spec.APIServer = tmp.Spec.APIServer
		}
		if tmp.Spec.Namespace != "" && tmp.Spec.Namespace != cfg.Spec.Namespace {
			fmt.Printf("updated spec.namespace (`%s -> `%s`)\n", cfg.Spec.Namespace, tmp.Spec.Namespace)
			cfg.Spec.Namespace = tmp.Spec.Namespace
		}
		if tmp.Spec.DiffStrategy != "" && tmp.Spec.DiffStrategy != cfg.Spec.DiffStrategy {
			fmt.Printf("updated spec.diffStrategy (`%s -> `%s`)\n", cfg.Spec.DiffStrategy, tmp.Spec.DiffStrategy)
			cfg.Spec.DiffStrategy = tmp.Spec.DiffStrategy
		}

		if err := writeJSON(cfg, filepath.Join(path, "spec.json")); err != nil {
			log.Fatalln(err)
		}
	}
	return cmd
}

func envAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [path]",
		Short: "create a new environment",
		Args:  cobra.ExactArgs(1),
		Annotations: map[string]string{
			"args": "dirs",
		},
	}
	cfg := v1alpha1.New()
	envSettingsFlags(cfg, cmd.Flags())
	cmd.Run = func(cmd *cobra.Command, args []string) {
		if err := addEnv(args[0], cfg); err != nil {
			log.Fatalln(err)
		}
	}
	return cmd
}

// used by initCmd() as well
func addEnv(dir string, cfg *v1alpha1.Config) error {
	path, err := filepath.Abs(dir)
	if err != nil {
		log.Fatalln(err)
	}
	if _, err := os.Stat(path); err != nil {
		// folder does not exist
		if os.IsNotExist(err) {
			os.MkdirAll(path, os.ModePerm)
		} else {
			// it exists
			if os.IsExist(err) {
				return fmt.Errorf("Directory %s already exists.", path)
			}
			// we have another error
			return fmt.Errorf("Creating directory: %s", err)
		}
	}

	// the other properties are already set by v1alpha1.New() and pflag.Parse()
	cfg.Metadata.Name = filepath.Base(path)

	// write spec.json
	if err := writeJSON(cfg, filepath.Join(path, "spec.json")); err != nil {
		log.Fatalln(err)
	}

	// write main.jsonnet
	if err := writeJSON(struct{}{}, filepath.Join(path, "main.jsonnet")); err != nil {
		log.Fatalln(err)
	}

	return nil
}

func envRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove [path]",
		Aliases: []string{"rm"},
		Short:   "delete an environment",
		Annotations: map[string]string{
			"args": "baseDir",
		},
		Run: func(cmd *cobra.Command, args []string) {
			for _, arg := range args {
				path, err := filepath.Abs(arg)
				if err != nil {
					log.Fatalln("parsing environments name:", err)
				}
				if err := util.Confirm(fmt.Sprintf("Permanently removing the environment located at '%s'.", path), "yes"); err != nil {
					log.Fatalln(err)
				}
				if err := os.RemoveAll(path); err != nil {
					log.Fatalf("Removing '%s': %s", path, err)
				}
				fmt.Println("Removed", path)
			}
		},
	}
}

func envListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "list environments",
		Args:    cobra.NoArgs,
	}

	cmd.Run = func(cmd *cobra.Command, args []string) {
		envs := []v1alpha1.Config{}
		dirs := findBaseDirs()
		useJson, err := cmd.Flags().GetBool("json")
		if err != nil {
			// this err should never occur. Panic in case
			panic(err)
		}
		for _, dir := range dirs {
			viper.Reset()
			envs = append(envs, *setupConfiguration(dir))
		}

		if useJson {
			j, err := json.Marshal(envs)
			if err != nil {
				log.Fatalln("Formatting as json:", j)
			}
			fmt.Println(string(j))
			return
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 4, ' ', 0)
		f := "%s\t%s\t%s\t\n"
		fmt.Fprintf(w, f, "NAME", "NAMESPACE", "SERVER")
		for _, e := range envs {
			fmt.Fprintf(w, f, e.Metadata.Name, e.Spec.Namespace, e.Spec.APIServer)
		}
		w.Flush()
	}
	return cmd
}
