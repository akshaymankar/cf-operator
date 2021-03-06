package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	yaml "gopkg.in/yaml.v2"

	"code.cloudfoundry.org/cf-operator/pkg/bosh/manifest"
)

// dataGatherCmd represents the dataGather command
var dataGatherCmd = &cobra.Command{
	Use:   "data-gather [flags]",
	Short: "Gathers data of a bosh manifest",
	Long: `Gathers data of a manifest. 

This will retrieve information of an instance-group
inside a bosh manifest file.

`,
	RunE: func(cmd *cobra.Command, args []string) error {
		log = newLogger()
		defer log.Sync()
		boshManifestPath := viper.GetString("bosh-manifest-path")
		if len(boshManifestPath) == 0 {
			return fmt.Errorf("manifest cannot be empty")
		}

		baseDir := viper.GetString("base-dir")
		if len(baseDir) == 0 {
			return fmt.Errorf("base directory cannot be empty")
		}

		namespace := viper.GetString("cf-operator-namespace")
		if len(namespace) == 0 {
			return fmt.Errorf("namespace cannot be empty")
		}

		instanceGroupName := viper.GetString("instance-group-name")
		if len(instanceGroupName) == 0 {
			return fmt.Errorf("instance-group-name cannot be empty")
		}

		boshManifestBytes, err := ioutil.ReadFile(boshManifestPath)
		if err != nil {
			return err
		}

		boshManifestStruct := manifest.Manifest{}
		err = yaml.Unmarshal(boshManifestBytes, &boshManifestStruct)
		if err != nil {
			return err
		}

		dg := manifest.NewDataGatherer(log, namespace, &boshManifestStruct)
		result, err := dg.GenerateManifest(baseDir, instanceGroupName)
		if err != nil {
			return err
		}

		jsonBytes, err := json.Marshal(map[string]string{
			"properties.yaml": string(result),
		})
		if err != nil {
			return errors.Wrapf(err, "could not marshal json output")
		}

		f := bufio.NewWriter(os.Stdout)
		defer f.Flush()
		_, err = f.Write(jsonBytes)
		if err != nil {
			return err
		}

		return nil
	},
}

func init() {
	utilCmd.AddCommand(dataGatherCmd)

	dataGatherCmd.Flags().StringP("base-dir", "b", "", "a path to the base directory")

	viper.BindPFlag("base-dir", dataGatherCmd.Flags().Lookup("base-dir"))

	argToEnv := map[string]string{
		"base-dir": "BASE_DIR",
	}
	AddEnvToUsage(dataGatherCmd, argToEnv)
}
