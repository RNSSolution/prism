package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	cfg "github.com/ColorPlatform/prism/config"
	cmn "github.com/ColorPlatform/prism/libs/common"
	"github.com/ColorPlatform/prism/p2p"
	"github.com/ColorPlatform/prism/privval"
	"github.com/ColorPlatform/prism/types"
	tmtime "github.com/ColorPlatform/prism/types/time"
)

// InitFilesCmd initialises a fresh Tendermint Core instance.
var InitFilesCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize Tendermint",
	RunE:  initFiles,
}

func initFiles(cmd *cobra.Command, args []string) error {
	return initFilesWithConfig(config)
}

func initFilesWithConfig(config *cfg.Config) error {
	// private validator
	privValKeyFile := config.PrivValidatorKeyFile()
	privValStateFile := config.PrivValidatorStateFile()
	var pv *privval.FilePV
	if cmn.FileExists(privValKeyFile) {
		pv = privval.LoadFilePV(privValKeyFile, privValStateFile)
		logger.Info("Found private validator", "keyFile", privValKeyFile,
			"stateFile", privValStateFile)
	} else {
		pv = privval.GenFilePV(privValKeyFile, privValStateFile)
		pv.Save()
		logger.Info("Generated private validator", "keyFile", privValKeyFile,
			"stateFile", privValStateFile)
	}

	nodeKeyFile := config.NodeKeyFile()
	if cmn.FileExists(nodeKeyFile) {
		logger.Info("Found node key", "path", nodeKeyFile)
	} else {
		if _, err := p2p.LoadOrGenNodeKey(nodeKeyFile); err != nil {
			return err
		}
		logger.Info("Generated node key", "path", nodeKeyFile)
	}

	// genesis file
	genFile := config.GenesisFile()
	if cmn.FileExists(genFile) {
		logger.Info("Found genesis file", "path", genFile)
	} else {
		genDoc := types.GenesisDoc{
			ChainID:         fmt.Sprintf("test-chain-%v", cmn.RandStr(6)),
			GenesisTime:     tmtime.Now(),
			ConsensusParams: types.DefaultConsensusParams(),
		}
		key := pv.GetPubKey()
		genDoc.Validators = []types.GenesisValidator{{
			Address: key.Address(),
			PubKey:  key,
			Power:   10,
			League:  0,
			NodeId:  0,
		}}

		if err := genDoc.SaveAs(genFile); err != nil {
			return err
		}
		logger.Info("Generated genesis file", "path", genFile)
	}

	return nil
}
