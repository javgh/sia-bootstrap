package main

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gitlab.com/NebulousLabs/Sia/node/api/client"

	"github.com/javgh/sia-bootstrap/httpreaderat"
)

const (
	retryInterval = 5 * time.Second
)

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

	temporaryConsensusLocation := fmt.Sprintf("%s.incomplete", consensusLocation)
	consensusFile, err := os.Create(temporaryConsensusLocation)
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

	err = os.Rename(temporaryConsensusLocation, consensusLocation)
	if err != nil {
		log.Fatal(err)
	}
}

func runPost(options client.Options) {
	err := readConfig()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Waiting for Sia daemon to become available... ")
	httpClient := client.New(options)
	for {
		_, err = httpClient.ConsensusGet()
		if err == nil {
			break
		}
		time.Sleep(retryInterval)
	}

	fmt.Println("Waiting for Sia daemon to sync... ")
	for {
		consensusStatus, err := httpClient.ConsensusGet()
		if err != nil {
			log.Fatal(err)
		}
		if consensusStatus.Synced {
			break
		}
		time.Sleep(retryInterval)
	}

	if !viper.GetBool("ensure_wallet_initialized") {
		return
	}

	walletStatus, err := httpClient.WalletGet()
	if err != nil {
		log.Fatal(err)
	}

	// Sia wallets are always encrypted. If it is reported as
	// unencrypted, it means that the wallet has not been initialized yet.
	if !walletStatus.Encrypted {
		if !viper.IsSet("wallet_password") {
			log.Fatal("Please set wallet_password to initialize wallet.")
		}
		walletPassword := viper.GetString("wallet_password")

		if viper.IsSet("wallet_seed") {
			fmt.Println("Initializing wallet with provided seed...")
			walletSeed := viper.GetString("wallet_seed")
			err = httpClient.WalletInitSeedPost(walletSeed, walletPassword, false)
			if err != nil {
				log.Fatal(err)
			}
		} else {
			fmt.Println("Initializing wallet with fresh seed...")
			_, err = httpClient.WalletInitPost(walletPassword, false)
			if err != nil {
				log.Fatal(err)
			}
		}
	}

	if !viper.GetBool("ensure_wallet_unlocked") {
		return
	}
	walletStatus, err = httpClient.WalletGet()
	if err != nil {
		log.Fatal(err)
	}

	if !walletStatus.Unlocked {
		if !viper.IsSet("wallet_password") {
			log.Fatal("Please set wallet_password to unlock wallet.")
		}
		walletPassword := viper.GetString("wallet_password")

		fmt.Println("Unlocking wallet...")
		err = httpClient.WalletUnlockPost(walletPassword)
		if err != nil {
			log.Fatal(err)
		}
	}

	if !viper.GetBool("ensure_recovery") {
		return
	}

	// Only go through the recovery process if
	// we don't already have (found) some contracts.
	rc, err := httpClient.RenterAllContractsGet()
	if err != nil {
		log.Fatal(err)
	}

	contractCount := len(rc.ActiveContracts) + len(rc.PassiveContracts) +
		len(rc.RefreshedContracts) + len(rc.DisabledContracts) + len(rc.ExpiredContracts) +
		len(rc.ExpiredRefreshedContracts) + len(rc.RecoverableContracts)
	if contractCount == 0 {
		fmt.Println("Triggering recovery scan...")
		err = httpClient.RenterInitContractRecoveryScanPost()
		if err != nil {
			log.Fatal(err)
		}

		// Wait for recovery scan to start.
		for {
			recoveryStatus, err := httpClient.RenterContractRecoveryProgressGet()
			if err != nil {
				log.Fatal(err)
			}

			if recoveryStatus.ScanInProgress {
				break
			}
			time.Sleep(retryInterval)
		}

		// Wait for recovery scan to complete.
		for {
			recoveryStatus, err := httpClient.RenterContractRecoveryProgressGet()
			if err != nil {
				log.Fatal(err)
			}

			if !recoveryStatus.ScanInProgress {
				break
			}
			time.Sleep(retryInterval)
		}
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

			runPost(options)
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
