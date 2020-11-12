package main

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gitlab.com/NebulousLabs/Sia/node/api/client"

	"github.com/javgh/sia-bootstrap/httpreaderat"
)

func debug(options client.Options) error {
	httpClient := client.New(options)

	consensusStatus, err := httpClient.ConsensusGet()
	if err != nil {
		return err
	}

	fmt.Println(consensusStatus)
	fmt.Println(consensusStatus.Synced)

	return nil
}

func readConfig() error {
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	appConfigDir := filepath.Join(userConfigDir, "sia-bootstrap")

	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(appConfigDir)
	return viper.ReadInConfig()
}

func runPre(cmd *cobra.Command, args []string) {
	err := readConfig()
	if err != nil {
		log.Fatal(err)
	}

	if !viper.IsSet("consensus_location") {
		return
	}

	consensusLocation, err := filepath.Abs(viper.GetString("consensus_location"))
	if err != nil {
		log.Fatal(err)
	}
	consensusLocationDir := filepath.Dir(consensusLocation)

	if !viper.IsSet("consensus_bootstrap") {
		return
	}
	consensusBootstrap := viper.GetString("consensus_bootstrap")

	// See if we can stat the consensus database.
	_, err = os.Stat(consensusLocation)
	if err == nil {
		// Consensus seems to be present - we can exit.
		return
	}

	// Consensus seems not to be present - ensure folders exist.
	err = os.MkdirAll(consensusLocationDir, 0700)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Consensus database not present at %s\n", consensusLocation)
	fmt.Printf("Downloading snapshot from %s\n", consensusBootstrap)
	hra, err := httpreaderat.New(consensusBootstrap)
	if err != nil {
		log.Fatal(err)
	}
	defer hra.Close()

	r, err := zip.NewReader(hra, hra.ContentLength)
	if err != nil {
		log.Fatal(err)
	}

	var consensusZipFile *zip.File
	for _, f := range r.File {
		if filepath.Base(f.Name) == "consensus.db" {
			consensusZipFile = f
			break
		}
	}
	if consensusZipFile == nil {
		log.Fatal("Bootstrap file does not seem to contain consensus.db")
	}

	rc, err := consensusZipFile.Open()
	if err != nil {
		log.Fatal(err)
	}

	consensusFile, err := os.Create(consensusLocation)
	if err != nil {
		log.Fatal(err)
	}

	_, err = io.Copy(consensusFile, rc)
	if err != nil {
		log.Fatal(err)
	}

	err = consensusFile.Close()
	if err != nil {
		log.Fatal(err)
	}

	err = rc.Close()
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	options, err := client.DefaultOptions()
	if err != nil {
		log.Fatal(err)
	}

	siaDaemonAddress := options.Address
	siaDaemonPassword := options.Password

	rootDesc := "Bootstrap the Sia daemon declaratively"
	rootCmd := &cobra.Command{
		Use:   "sia-bootstrap",
		Short: rootDesc,
		Long:  fmt.Sprintf("%s.", rootDesc),
	}

	descPre := "Run this before starting the Sia daemon"
	preCmd := &cobra.Command{
		Use:   "pre",
		Short: descPre,
		Long:  fmt.Sprintf("%s.", descPre),
		Run:   runPre,
	}

	descPost := "Run this after starting the Sia daemon"
	postCmd := &cobra.Command{
		Use:   "post",
		Short: descPost,
		Long:  fmt.Sprintf("%s.", descPost),
		Run: func(cmd *cobra.Command, args []string) {
			options.Address = siaDaemonAddress
			options.Password = siaDaemonPassword

			err := debug(options)
			if err != nil {
				log.Fatal(err)
			}
		},
	}

	rootCmd.AddCommand(preCmd, postCmd)
	rootCmd.PersistentFlags().StringVar(&siaDaemonPassword, "sia-password", siaDaemonPassword,
		"Sia API password")
	rootCmd.PersistentFlags().StringVar(&siaDaemonAddress, "sia-daemon", siaDaemonAddress,
		"host and port of Sia daemon")

	err = rootCmd.Execute()
	if err != nil {
		log.Fatal(err)
	}
}
