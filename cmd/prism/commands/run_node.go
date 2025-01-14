package commands

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	cmn "github.com/ColorPlatform/prism/libs/common"
	nm "github.com/ColorPlatform/prism/node"

	"github.com/ColorPlatform/prism/globals"
	global_leagues "github.com/ColorPlatform/prism/globals/leagues"
	global_logger "github.com/ColorPlatform/prism/globals/logger"
)

// AddNodeFlags exposes some common configuration options on the command-line
// These are exposed for convenience of commands embedding a tendermint node
func AddNodeFlags(cmd *cobra.Command) {
	// bind flags
	cmd.Flags().String("moniker", config.Moniker, "Node Name")

	// priv val flags
	cmd.Flags().String("priv_validator_laddr", config.PrivValidatorListenAddr, "Socket address to listen on for connections from external priv_validator process")

	// node flags
	cmd.Flags().Bool("fast_sync", config.FastSync, "Fast blockchain syncing")

	// abci flags
	cmd.Flags().String("proxy_app", config.ProxyApp, "Proxy app address, or one of: 'kvstore', 'persistent_kvstore', 'counter', 'counter_serial' or 'noop' for local testing.")
	cmd.Flags().String("abci", config.ABCI, "Specify abci transport (socket | grpc)")

	// rpc flags
	cmd.Flags().String("rpc.laddr", config.RPC.ListenAddress, "RPC listen address. Port required")
	cmd.Flags().String("rpc.grpc_laddr", config.RPC.GRPCListenAddress, "GRPC listen address (BroadcastTx only). Port required")
	cmd.Flags().Bool("rpc.unsafe", config.RPC.Unsafe, "Enabled unsafe rpc methods")

	// p2p flags
	cmd.Flags().String("p2p.laddr", config.P2P.ListenAddress, "Node listen address. (0.0.0.0:0 means any interface, any port)")
	cmd.Flags().String("p2p.seeds", config.P2P.Seeds, "Comma-delimited ID@host:port seed nodes")
	cmd.Flags().String("p2p.persistent_peers", config.P2P.PersistentPeers, "Comma-delimited ID@host:port persistent peers")
	cmd.Flags().Bool("p2p.upnp", config.P2P.UPNP, "Enable/disable UPNP port forwarding")
	cmd.Flags().Bool("p2p.pex", config.P2P.PexReactor, "Enable/disable Peer-Exchange")
	cmd.Flags().Bool("p2p.seed_mode", config.P2P.SeedMode, "Enable/disable seed mode")
	cmd.Flags().String("p2p.private_peer_ids", config.P2P.PrivatePeerIDs, "Comma-delimited private peer IDs")

	// consensus flags
	cmd.Flags().Bool("consensus.create_empty_blocks", config.Consensus.CreateEmptyBlocks, "Set this to false to only produce blocks when there are txs or when the AppHash changes")
	cmd.Flags().Bool("consensus.use_leagues", config.Consensus.UseLeagues, "Set this to false to switch to Tendermint consensus")

	cmd.Flags().Int64VarP(&globals.StartTimestamp, "consensus.start", "", 0, "Start time for Round 0 in seconds since epoch (use date '+%s'")

}

// NewRunNodeCmd returns the command that allows the CLI to start a node.
// It can be used with a custom PrivValidator and in-process ABCI application.
func NewRunNodeCmd(nodeProvider nm.NodeProvider) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Run the tendermint node",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger.Debug("globals.StartTimestamp", globals.StartTimestamp, "now", time.Now().Unix(), "secondsLeft")
			if globals.StartTimestamp > 0 {
				logger.Debug("Waiting for consensus start", "seconds", globals.StartTimestamp - time.Now().Unix())
			}

			n, err := nodeProvider(config, logger)
			if err != nil {
				return fmt.Errorf("Failed to create node: %v", err)
			}

			nm.SetSelf(n)
			global_leagues.DefineLeagues(n.Leagues())
			globals.SetLeague(config.Consensus.League)
			globals.SetNodeId(config.Consensus.NodeId)
			global_logger.SetLogger(logger.With("module", "unspecified"))

			// Stop upon receiving SIGTERM or CTRL-C.
			cmn.TrapSignal(logger, func() {
				if n.IsRunning() {
					n.Stop()
				}
			})

			if err := n.Start(); err != nil {
				return fmt.Errorf("Failed to start node: %v", err)
			}
			logger.Info("Started node", "nodeInfo", n.Switch().NodeInfo())

			// Run forever.
			select {}
		},
	}

	AddNodeFlags(cmd)
	return cmd
}
